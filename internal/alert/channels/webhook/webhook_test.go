package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colt005/kivert/internal/alert"
)

func TestWebhookAlerter_Send_DefaultJSON(t *testing.T) {
	var receivedBody string
	var authHeader string
	var customHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		authHeader = r.Header.Get("Authorization")
		customHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := map[string]any{
		"url":            server.URL,
		"method":         "POST",
		"timeoutSeconds": 2,
		"retries":        1,
		"headers": map[string]any{
			"X-Custom-Header": "CustomValue",
		},
	}

	alerter, err := NewWebhookAlerter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Set a mock environment variable for token
	t.Setenv("KIVERT_WEBHOOK_TOKEN", "mock-token-123")
	// Re-build to pick up the token
	alerter, err = NewWebhookAlerter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	a := alert.Alert{
		Namespace:    "ns1",
		Pod:          "pod1",
		Container:    "c1",
		RestartCount: 3,
		Reason:       "OOMKilled",
		Timestamp:    time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
	}

	err = alerter.Send(context.Background(), a)
	if err != nil {
		t.Fatalf("expected send to succeed, got %v", err)
	}

	// Verify Headers
	if authHeader != "Bearer mock-token-123" {
		t.Fatalf("expected Authorization header Bearer mock-token-123, got %q", authHeader)
	}
	if customHeader != "CustomValue" {
		t.Fatalf("expected custom header value CustomValue, got %q", customHeader)
	}

	// Verify body parses back to correct struct
	var parsed alert.Alert
	if err := json.Unmarshal([]byte(receivedBody), &parsed); err != nil {
		t.Fatalf("failed to parse received JSON body: %v", err)
	}

	if parsed.Pod != "pod1" || parsed.RestartCount != 3 || parsed.Reason != "OOMKilled" {
		t.Fatalf("unexpected parsed alert details: %+v", parsed)
	}
}

func TestWebhookAlerter_Send_Template(t *testing.T) {
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := map[string]any{
		"url":      server.URL,
		"template": `{"text":"Pod {{.Pod}} in {{.Namespace}} restarted {{.RestartCount}} times due to {{.Reason}}."}`,
	}

	alerter, err := NewWebhookAlerter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	a := alert.Alert{
		Namespace:    "kivert-system",
		Pod:          "kivert-controller",
		Container:    "manager",
		RestartCount: 5,
		Reason:       "CrashLoopBackOff",
	}

	err = alerter.Send(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}

	expectedText := `{"text":"Pod kivert-controller in kivert-system restarted 5 times due to CrashLoopBackOff."}`
	if receivedBody != expectedText {
		t.Fatalf("expected template body to be %q, got %q", expectedText, receivedBody)
	}
}

func TestWebhookAlerter_Send_Retries(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := map[string]any{
		"url":            server.URL,
		"retries":        3,
		"timeoutSeconds": 1,
	}

	alerter, err := NewWebhookAlerter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	a := alert.Alert{Pod: "pod-retry"}

	err = alerter.Send(context.Background(), a)
	if err != nil {
		t.Fatalf("expected send to eventually succeed after retries, got %v", err)
	}

	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount != 3 {
		t.Fatalf("expected 3 total requests, got %d", finalCount)
	}
}
