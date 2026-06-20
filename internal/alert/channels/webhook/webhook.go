package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/colt005/kivert/internal/alert"
)

const alerterKind = "webhook"

func init() {
	alert.Register(alerterKind, NewWebhookAlerter)
}

// WebhookAlerter sends alert JSON payloads to an external HTTP endpoint.
type WebhookAlerter struct {
	url         string
	method      string
	timeout     time.Duration
	retries     int
	headers     map[string]string
	token       string
	tmpl        *template.Template
	client      *http.Client
}

// NewWebhookAlerter builds a WebhookAlerter from a configuration map.
func NewWebhookAlerter(cfg map[string]any) (alert.Alerter, error) {
	url := getMapString(cfg, "url", "")
	if url == "" {
		return nil, fmt.Errorf("webhook alerter: 'url' config is required")
	}

	method := strings.ToUpper(getMapString(cfg, "method", "POST"))
	timeoutSecs := getMapInt(cfg, "timeoutSeconds", 5)
	retries := getMapInt(cfg, "retries", 3)
	headers := getMapStringMap(cfg, "headers")

	// Resolve authorization token
	var token string
	if authSecretRef, ok := cfg["authSecretRef"].(map[string]any); ok {
		secretName := getMapString(authSecretRef, "name", "")
		secretKey := getMapString(authSecretRef, "key", "token")
		if secretName != "" {
			// Secret volume mounts use the secret name as the directory and the key as the filename
			path := fmt.Sprintf("/etc/kivert/secrets/%s/%s", secretName, secretKey)
			if data, err := os.ReadFile(path); err == nil {
				token = strings.TrimSpace(string(data))
			}
		}
	}

	// Fallback to environment variable if token was not loaded from file
	if token == "" {
		token = os.Getenv("KIVERT_WEBHOOK_TOKEN")
	}

	var tmpl *template.Template
	templateStr := getMapString(cfg, "template", "")
	if templateStr != "" {
		var err error
		tmpl, err = template.New("webhook").Parse(templateStr)
		if err != nil {
			return nil, fmt.Errorf("webhook alerter: invalid Go template: %w", err)
		}
	}

	return &WebhookAlerter{
		url:     url,
		method:  method,
		timeout: time.Duration(timeoutSecs) * time.Second,
		retries: retries,
		headers: headers,
		token:   token,
		tmpl:    tmpl,
		client: &http.Client{
			Timeout: time.Duration(timeoutSecs) * time.Second,
		},
	}, nil
}

// Name returns the identifier of this alerter kind.
func (w *WebhookAlerter) Name() string {
	return alerterKind
}

// Send dispatches the alert using custom templates (if configured), sets headers/auth, and retries on failure.
func (w *WebhookAlerter) Send(ctx context.Context, a alert.Alert) error {
	var payloadBytes []byte
	var err error

	if w.tmpl != nil {
		var buf bytes.Buffer
		if err := w.tmpl.Execute(&buf, a); err != nil {
			return fmt.Errorf("failed to execute webhook template: %w", err)
		}
		payloadBytes = buf.Bytes()
	} else {
		payloadBytes, err = json.Marshal(a)
		if err != nil {
			return fmt.Errorf("failed to marshal alert to JSON: %w", err)
		}
	}

	var lastErr error
	backoff := 100 * time.Millisecond

	for i := 0; i <= w.retries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		lastErr = w.executeRequest(ctx, payloadBytes)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to send webhook after %d retries: %w", w.retries, lastErr)
}

func (w *WebhookAlerter) executeRequest(ctx context.Context, body []byte) error {
	reqCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, w.method, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to construct request: %w", err)
	}

	// Apply custom headers
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}

	// Apply JSON header if not already specified and using a template-free run
	if req.Header.Get("Content-Type") == "" && w.tmpl == nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply authorization token if resolved
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("received non-2xx status code %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Helpers for safe map type conversions.

func getMapString(m map[string]any, key string, defaultValue string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return defaultValue
}

func getMapInt(m map[string]any, key string, defaultValue int) int {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return defaultValue
}

func getMapStringMap(m map[string]any, key string) map[string]string {
	res := make(map[string]string)
	if val, ok := m[key]; ok {
		if subMap, ok := val.(map[string]any); ok {
			for k, v := range subMap {
				if s, ok := v.(string); ok {
					res[k] = s
				}
			}
		}
	}
	return res
}
