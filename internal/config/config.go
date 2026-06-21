package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

// WatchConfig defines configuration for watching Pods.
type WatchConfig struct {
	AllNamespaces     bool     `json:"allNamespaces" yaml:"allNamespaces"`
	Namespaces        []string `json:"namespaces" yaml:"namespaces"`
	ExcludeNamespaces []string `json:"excludeNamespaces" yaml:"excludeNamespaces"`
	LabelSelector     string   `json:"labelSelector" yaml:"labelSelector"`
}

// AlertingConfig defines restart thresholds, cooldowns, and dry-run options.
type AlertingConfig struct {
	RestartThreshold int32    `json:"restartThreshold" yaml:"restartThreshold"`
	CooldownSeconds  int32    `json:"cooldownSeconds" yaml:"cooldownSeconds"`
	IncludeReasons   []string `json:"includeReasons" yaml:"includeReasons"`
	DryRun           bool     `json:"dryRun" yaml:"dryRun"`
}

// LogsConfig defines tail rules, limits, timeouts, and redaction patterns.
type LogsConfig struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	TailLines      int64    `json:"tailLines" yaml:"tailLines"`
	Previous       bool     `json:"previous" yaml:"previous"`
	LimitBytes     int64    `json:"limitBytes" yaml:"limitBytes"`
	TimeoutSeconds int64    `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	IncludeInAlert bool     `json:"includeInAlert" yaml:"includeInAlert"`
	RedactPatterns []string `json:"redactPatterns" yaml:"redactPatterns"`
}

// ChannelConfig represents an alert destination.
type ChannelConfig struct {
	Name    string         `json:"name" yaml:"name"`
	Type    string         `json:"type" yaml:"type"`
	Enabled bool           `json:"enabled" yaml:"enabled"`
	Config  map[string]any `json:"config" yaml:"config"`
}

// MetricsConfig defines configuration for metrics collection.
type MetricsConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
	Port    int  `json:"port" yaml:"port"`
}

// ControllerConfig defines options for leader election, caching, and logs.
type ControllerConfig struct {
	LeaderElection      bool          `json:"leaderElection" yaml:"leaderElection"`
	ResyncPeriodSeconds int           `json:"resyncPeriodSeconds" yaml:"resyncPeriodSeconds"`
	LogLevel            string        `json:"logLevel" yaml:"logLevel"`
	Metrics             MetricsConfig `json:"metrics" yaml:"metrics"`
}

// Config is the top-level configuration object.
type Config struct {
	Watch      WatchConfig      `json:"watch" yaml:"watch"`
	Alerting   AlertingConfig   `json:"alerting" yaml:"alerting"`
	Logs       LogsConfig       `json:"logs" yaml:"logs"`
	Channels   []ChannelConfig  `json:"channels" yaml:"channels"`
	Controller ControllerConfig `json:"controller" yaml:"controller"`
}

// NewDefaultConfig returns a Config struct initialized with defaults.
func NewDefaultConfig() *Config {
	return &Config{
		Watch: WatchConfig{
			AllNamespaces:     false,
			Namespaces:        []string{"default"},
			ExcludeNamespaces: []string{"kube-system"},
			LabelSelector:     "",
		},
		Alerting: AlertingConfig{
			RestartThreshold: 1,
			CooldownSeconds:  300,
			IncludeReasons:   []string{},
			DryRun:           false,
		},
		Logs: LogsConfig{
			Enabled:        false,
			TailLines:      50,
			Previous:       true,
			LimitBytes:     65536,
			TimeoutSeconds: 5,
			IncludeInAlert: true,
			RedactPatterns: []string{},
		},
		Channels: []ChannelConfig{},
		Controller: ControllerConfig{
			LeaderElection:      true,
			ResyncPeriodSeconds: 0,
			LogLevel:            "info",
			Metrics: MetricsConfig{
				Enabled: true,
				Port:    8080,
			},
		},
	}
}

// Load parses configuration from command line flags, environment variables,
// and optionally from a mounted config file.
func Load() (*Config, error) {
	cfg := NewDefaultConfig()

	// 1. Define command line flags.
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to config file (JSON or YAML).")

	// Temporarily define other flags for overrides.
	flag.BoolVar(&cfg.Watch.AllNamespaces, "watch-all-namespaces", cfg.Watch.AllNamespaces, "Watch all namespaces.")
	flag.Int64Var(&cfg.Logs.TailLines, "logs-tail-lines", cfg.Logs.TailLines, "Tail lines to fetch.")
	flag.BoolVar(&cfg.Controller.LeaderElection, "leader-election", cfg.Controller.LeaderElection, "Enable leader election.")
	flag.IntVar(&cfg.Controller.Metrics.Port, "metrics-port", cfg.Controller.Metrics.Port, "Port to expose metrics on.")
	flag.StringVar(&cfg.Controller.LogLevel, "log-level", cfg.Controller.LogLevel, "Log level.")

	flag.Parse()

	// Env var check for config path.
	if envConfigPath := os.Getenv("KIVERT_CONFIG"); envConfigPath != "" {
		configPath = envConfigPath
	}

	// 2. Load from config file if specified.
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %q: %w", configPath, err)
		}

		// Support both JSON and YAML by checking extension or trying YAML (which parses JSON too).
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// 3. Override with environment variables (KIVERT_*).
	overrideWithEnv(cfg)

	return cfg, nil
}

func overrideWithEnv(cfg *Config) {
	if val, ok := lookupEnvBool("KIVERT_WATCH_ALLNAMESPACES"); ok {
		cfg.Watch.AllNamespaces = val
	}
	if val, ok := lookupEnvSlice("KIVERT_WATCH_NAMESPACES"); ok {
		cfg.Watch.Namespaces = val
	}
	if val, ok := lookupEnvSlice("KIVERT_WATCH_EXCLUDE_NAMESPACES"); ok {
		cfg.Watch.ExcludeNamespaces = val
	}
	if val := os.Getenv("KIVERT_WATCH_LABELSELECTOR"); val != "" {
		cfg.Watch.LabelSelector = val
	}

	if val, ok := lookupEnvInt32("KIVERT_ALERTING_RESTARTTHRESHOLD"); ok {
		cfg.Alerting.RestartThreshold = val
	}
	if val, ok := lookupEnvInt32("KIVERT_ALERTING_COOLDOWNSECONDS"); ok {
		cfg.Alerting.CooldownSeconds = val
	}
	if val, ok := lookupEnvSlice("KIVERT_ALERTING_INCLUDEREASONS"); ok {
		cfg.Alerting.IncludeReasons = val
	}
	if val, ok := lookupEnvBool("KIVERT_ALERTING_DRYRUN"); ok {
		cfg.Alerting.DryRun = val
	}

	if val, ok := lookupEnvBool("KIVERT_LOGS_ENABLED"); ok {
		cfg.Logs.Enabled = val
	}
	if val, ok := lookupEnvInt64("KIVERT_LOGS_TAILLINES"); ok {
		cfg.Logs.TailLines = val
	}
	if val, ok := lookupEnvBool("KIVERT_LOGS_PREVIOUS"); ok {
		cfg.Logs.Previous = val
	}
	if val, ok := lookupEnvInt64("KIVERT_LOGS_LIMITBYTES"); ok {
		cfg.Logs.LimitBytes = val
	}
	if val, ok := lookupEnvInt64("KIVERT_LOGS_TIMEOUTSECONDS"); ok {
		cfg.Logs.TimeoutSeconds = val
	}
	if val, ok := lookupEnvBool("KIVERT_LOGS_INCLUDEINALERT"); ok {
		cfg.Logs.IncludeInAlert = val
	}
	if val, ok := lookupEnvSlice("KIVERT_LOGS_REDACTPATTERNS"); ok {
		cfg.Logs.RedactPatterns = val
	}

	if val, ok := lookupEnvBool("KIVERT_CONTROLLER_LEADERELECTION"); ok {
		cfg.Controller.LeaderElection = val
	}
	if val, ok := lookupEnvInt("KIVERT_CONTROLLER_RESYNCPERIODSECONDS"); ok {
		cfg.Controller.ResyncPeriodSeconds = val
	}
	if val := os.Getenv("KIVERT_CONTROLLER_LOGLEVEL"); val != "" {
		cfg.Controller.LogLevel = val
	}
	if val, ok := lookupEnvBool("KIVERT_CONTROLLER_METRICS_ENABLED"); ok {
		cfg.Controller.Metrics.Enabled = val
	}
	if val, ok := lookupEnvInt("KIVERT_CONTROLLER_METRICS_PORT"); ok {
		cfg.Controller.Metrics.Port = val
	}

	// Support loading channels from JSON array in env.
	if val := os.Getenv("KIVERT_CHANNELS"); val != "" {
		var envChannels []ChannelConfig
		if err := json.Unmarshal([]byte(val), &envChannels); err == nil {
			cfg.Channels = envChannels
		}
	}
}

func lookupEnvBool(key string) (bool, bool) {
	val := os.Getenv(key)
	if val == "" {
		return false, false
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return false, false
	}
	return b, true
}

func lookupEnvInt(key string) (int, bool) {
	val := os.Getenv(key)
	if val == "" {
		return 0, false
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	return i, true
}

func lookupEnvInt32(key string) (int32, bool) {
	val := os.Getenv(key)
	if val == "" {
		return 0, false
	}
	i, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(i), true
}

func lookupEnvInt64(key string) (int64, bool) {
	val := os.Getenv(key)
	if val == "" {
		return 0, false
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false
	}
	return i, true
}

func lookupEnvSlice(key string) ([]string, bool) {
	val := os.Getenv(key)
	if val == "" {
		return nil, false
	}
	parts := strings.Split(val, ",")
	var cleaned []string
	for _, p := range parts {
		cleaned = append(cleaned, strings.TrimSpace(p))
	}
	return cleaned, true
}
