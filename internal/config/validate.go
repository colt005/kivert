package config

import (
	"errors"
	"fmt"
	"regexp"
)

// Validate checks the configuration for correctness and returns an error if invalid.
func (c *Config) Validate() error {
	// 1. Validate Watch config
	if !c.Watch.AllNamespaces && len(c.Watch.Namespaces) == 0 {
		return errors.New("watch.namespaces cannot be empty when watch.allNamespaces is false")
	}

	// 2. Validate Alerting config
	if c.Alerting.RestartThreshold < 0 {
		return fmt.Errorf("alerting.restartThreshold must be >= 0, got %d", c.Alerting.RestartThreshold)
	}
	if c.Alerting.CooldownSeconds < 0 {
		return fmt.Errorf("alerting.cooldownSeconds must be >= 0, got %d", c.Alerting.CooldownSeconds)
	}

	// 3. Validate Logs config
	if c.Logs.Enabled {
		if c.Logs.TailLines <= 0 {
			return fmt.Errorf("logs.tailLines must be > 0 when logs are enabled, got %d", c.Logs.TailLines)
		}
		if c.Logs.LimitBytes <= 0 {
			return fmt.Errorf("logs.limitBytes must be > 0 when logs are enabled, got %d", c.Logs.LimitBytes)
		}
		if c.Logs.TimeoutSeconds <= 0 {
			return fmt.Errorf("logs.timeoutSeconds must be > 0 when logs are enabled, got %d", c.Logs.TimeoutSeconds)
		}
		for _, pattern := range c.Logs.RedactPatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid regex pattern in logs.redactPatterns %q: %w", pattern, err)
			}
		}
	}

	// 4. Validate Channels config
	for i, channel := range c.Channels {
		if channel.Enabled {
			if channel.Name == "" {
				return fmt.Errorf("channel[%d]: name cannot be empty", i)
			}
			if channel.Type == "" {
				return fmt.Errorf("channel[%d] (%s): type cannot be empty", i, channel.Name)
			}
			if channel.Type == "webhook" {
				urlVal, ok := channel.Config["url"]
				if !ok || urlVal == "" {
					return fmt.Errorf("channel[%d] (%s): webhook configuration must include a non-empty 'url'", i, channel.Name)
				}
			}
		}
	}

	// 5. Validate Controller config
	if c.Controller.Metrics.Enabled {
		if c.Controller.Metrics.Port <= 0 || c.Controller.Metrics.Port > 65535 {
			return fmt.Errorf("controller.metrics.port must be between 1 and 65535, got %d", c.Controller.Metrics.Port)
		}
	}

	switch c.Controller.LogLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("invalid controller.logLevel %q (must be debug, info, warn, or error)", c.Controller.LogLevel)
	}

	return nil
}
