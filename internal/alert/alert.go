package alert

import (
	"time"
)

// Alert represents the data contract for a pod container restart event.
type Alert struct {
	Namespace     string    `json:"namespace"`
	Pod           string    `json:"pod"`
	Container     string    `json:"container"`
	Node          string    `json:"node"`
	Image         string    `json:"image"`
	RestartCount  int32     `json:"restartCount"`
	Reason        string    `json:"reason"` // e.g. CrashLoopBackOff, OOMKilled, Error
	ExitCode      int32     `json:"exitCode"`
	Owner         string    `json:"owner"` // resolved owner, e.g. "Deployment/payments-api"
	Message       string    `json:"message"`
	Logs          string    `json:"logs,omitempty"`
	LogsTruncated bool      `json:"logsTruncated,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}
