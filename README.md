# Kivert — Kubernetes Pod-Restart Alerting Controller

  Kivert is a lightweight, production-grade Kubernetes controller that watches for container restart events and forwards them to configured alert channels (e.g. webhooks).

## Core Design Principles

1. **Watch, Never Poll**: Kivert uses standard informer list-watches (`controller-runtime` cache) to monitor Pod status updates. It never polls the Kubernetes API on a ticker.
2. **Boot-Storm Prevention**: On startup, Kivert queries existing Pod status and seeds the internal baseline store with current restart counts. No alerts are emitted for restarts that occurred prior to controller startup. Only restart count increases observed *after* startup trigger alerts.
3. **High Performance Predicates**: A fast predicate gate filters out Pod updates where container restart counts have not changed, discarding the vast majority of status churn (IP changes, conditions, etc.) before they hit the controller queue.
4. **HA-Safe**: Out-of-the-box leader election support allows running multiple replicas safely without duplicate alerts.
5. **Decoupled Architecture**: Pluggable alert channels. The core controller has zero compile-time dependency on concrete channel packages, which register themselves dynamically using a factory pattern.

---

## Installation via Helm

Install Kivert into the `kivert-system` namespace:

```bash
helm upgrade --install kivert ./charts/kivert \
  --namespace kivert-system \
  --create-namespace
```

---

## Helm Chart Configuration (values.yaml)

### Watch Options
| Value | Default | Description |
|---|---|---|
| `watch.allNamespaces` | `false` | If true, watches Pods across all namespaces. Otherwise, watches only listed namespaces. |
| `watch.namespaces` | `["default"]` | Array of namespaces to watch when `allNamespaces` is false. |
| `watch.excludeNamespaces` | `["kube-system"]` | Array of namespaces to exclude from alerting. |
| `watch.labelSelector` | `""` | Kubernetes label selector to target specific Pods (e.g. `env=prod`). |

### Alerting Policies
| Value | Default | Description |
|---|---|---|
| `alerting.restartThreshold` | `1` | Alert on delta restart counts equal to or greater than this threshold. |
| `alerting.cooldownSeconds` | `300` | Cooldown period per `(namespace, pod, container)` to prevent alert flooding. |
| `alerting.includeReasons` | `[]` | List of termination reasons to alert on (e.g. `OOMKilled`, `Error`). Empty list means alert on all reasons. |
| `alerting.dryRun` | `false` | If true, logs the alerts but does not transmit them to configured channels. |

### Log Enrichment (Opt-in)
| Value | Default | Description |
|---|---|---|
| `logs.enabled` | `false` | **Opt-in** flag. If true, grants the controller `pods/log` RBAC permissions to fetch container logs. |
| `logs.tailLines` | `50` | Maximum number of lines to fetch. |
| `logs.previous` | `true` | Fetch logs of the previous (crashed) container instance. |
| `logs.limitBytes` | `65536` | Strict buffer limit cap for fetched logs. |
| `logs.timeoutSeconds` | `5` | Request timeout limit when retrieving logs from the API server. |
| `logs.includeInAlert` | `true` | If true, attaches the fetched logs to the outbound alert payload. |
| `logs.redactPatterns` | `[]` | Regex patterns applied line-by-line to redact sensitive information (secrets, PII) from logs. |

### Global Settings & Operations
| Value | Default | Description |
|---|---|---|
| `controller.leaderElection` | `true` | Enable leader election lease locking for high availability. |
| `controller.resyncPeriodSeconds` | `0` | Caching resync period. Set to `0` to disable periodical informer cache replays. |
| `controller.logLevel` | `"info"` | Log level (`debug`, `info`, `warn`, `error`). |
| `controller.metrics.enabled` | `true` | Expose Prometheus metrics endpoint. |
| `controller.metrics.port` | `8080` | Metrics binding port. |
| `image.repository` | `ghcr.io/colt005/kivert` | Container image repository path. |
| `image.tag` | `""` | Container image tag (defaults to `.Chart.AppVersion`). |
| `image.pullPolicy` | `IfNotPresent` | Container image pull policy. |
| `replicaCount` | `1` | Number of running replicas. Safe to scale up (uses leader election). |
| `serviceAccount.create` | `true` | Create a custom ServiceAccount. |
| `rbac.create` | `true` | Create Role/ClusterRole and Bindings. |
| `serviceMonitor.enabled` | `false` | Create a Prometheus Operator `ServiceMonitor` resource. |

---

## Alerting Channels Configuration (`channels`)

Alert destinations are configured in a structured array under `channels`.

```yaml
channels:
  - name: primary-webhook
    type: webhook
    enabled: true
    config:
      url: "http://httpbin.org/post"
      method: POST
      timeoutSeconds: 5
      retries: 3
      headers:
        Content-Type: application/json
      authSecretRef:
        name: ""
        key: token
      template: ""
```

### Channel Value Descriptions

- **`name`** (string): Logical unique identifier for this channel (used in logging and metrics).
- **`type`** (string): Type of alert destination (currently `webhook` is supported).
- **`enabled`** (bool): Toggle to activate/deactivate the channel.
- **`config.url`** (string): The HTTP destination URL (required).
- **`config.method`** (string): HTTP method to use (e.g. `POST`, `PUT`). Defaults to `POST`.
- **`config.timeoutSeconds`** (int): Request timeout. Defaults to `5`.
- **`config.retries`** (int): Number of retries on network failures. Employs exponential backoff. Defaults to `3`.
- **`config.headers`** (map[string]string): Map of custom HTTP headers sent with the payload.
- **`config.authSecretRef`** (object): Reference to a Kubernetes Secret containing a bearer authorization token.
  - **`name`** (string): Name of the secret.
  - **`key`** (string): The secret key containing the token. Defaults to `token`.
- **`config.template`** (string): Go-template syntax string used to shape the JSON payload. If empty, the raw `Alert` JSON representation is sent.
  - *Example custom template:*
    `{"text": "Container {{.Container}} in pod {{.Pod}} restarted! Reason: {{.Reason}}"}`

---

## Security Notice: Log Enrichment and Redaction

> [!WARNING]
> Enabling `logs.enabled: true` grants Kivert read permissions to container logs. Logs are fetched, attached to alert payloads, and shipped to configured external channels (like webhooks). These logs may contain sensitive information, API keys, or PII.
>
> To secure your data:
> 1. Keep `logs.enabled` set to `false` if log context is not required.
> 2. Define regex patterns under `logs.redactPatterns` to strip sensitive information before transmission. For example:
>    ```yaml
>    logs:
>      redactPatterns:
>        - "(?i)password\\s*=\\s*[^\\s]+"
>        - "bearer\\s+[^\\s]+"
>    ```

---

## Extending Kivert: Adding a New Alert Channel

Adding a new alert channel (e.g. Slack, PagerDuty, Email) is fully modular. The core controller depends only on the `alert.Alert` struct and `alert.Alerter` interface.

### Step 1: Create a new package
Create a new file `internal/alert/channels/slack/slack.go`:

```go
package slack

import (
	"context"
	"fmt"

	"github.com/colt005/kivert/internal/alert"
)

func init() {
	// Register the channel factory under the "slack" type key
	alert.Register("slack", NewSlackAlerter)
}

type SlackAlerter struct {
	webhookURL string
}

func NewSlackAlerter(cfg map[string]any) (alert.Alerter, error) {
	url, _ := cfg["webhookUrl"].(string)
	if url == "" {
		return nil, fmt.Errorf("slack channel requires webhookUrl")
	}
	return &SlackAlerter{webhookURL: url}, nil
}

func (s *SlackAlerter) Name() string {
	return "slack"
}

func (s *SlackAlerter) Send(ctx context.Context, a alert.Alert) error {
	// Implement sending logic here...
	return nil
}
```

### Step 2: Import the package in main.go
Simply add a blank import to `cmd/manager/main.go` so the package's `init()` function runs:

```go
import (
	_ "github.com/colt005/kivert/internal/alert/channels/slack"
)
```

No core files need to be modified, ensuring clean separation of concerns and compile-time decoupling.

---

## Roadmap & Future Features

We plan to expand Kivert with the following production-grade features:

### 🚀 1. Per-Pod Configurations via Annotations (Self-Service)
Allow application developers to customize Kivert alerting behavior on a per-workload basis by annotating their workloads:
- `kivert.io/restart-threshold`: Override the global restart threshold.
- `kivert.io/cooldown-seconds`: Override the default cooldown window.
- `kivert.io/mute`: Silence Kivert alerts entirely for a specific workload.
- `kivert.io/channel`: Route alerts for a specific pod to a targeted alert channel.

### 🔌 2. Native Slack & PagerDuty Channels
Extend the pluggable registry pattern by shipping out-of-the-box support for:
- **Slack**: Format alerts into rich Slack Block Kit layouts with status color bars, structured fields, and log attachments.
- **PagerDuty**: Integrate with PagerDuty Events API v2 to trigger, update, and resolve incidents.

### 🧹 3. Log Formatting & ANSI Escape Code Stripping
Improve log readability in downstream channels:
- Automatically strip ANSI color codes from terminal logs.
- Detect and pretty-print JSON log lines or format stack traces before sending alerts.

### 📢 4. Alert Aggregation & Rollup (Anti-Fatigue)
To prevent alert fatigue in large-scale environments:
- **Deployment-Level Summaries**: If multiple pods belonging to the same owner (e.g. `Deployment/payments-api`) crash-loop simultaneously (e.g. after a bad release), aggregate them into a single alert summary instead of flooding channels with separate messages.

### 🔗 5. Direct Observability Links (Contextual Navigation)
Provide rapid debugging context by embedding clickable links directly in the alert payloads:
- Configure dynamic URL templates (e.g. `grafanaUrlTemplate: "https://grafana.corp/d/pods?var-pod={{.Pod}}"`).
- Render and attach links to Grafana metrics, Kibana/Datadog logs, or cloud provider dashboards corresponding to the specific pod and namespace.


