---
sidebar_position: 19
---

# Kubernetes Events (input)

## Overview

`snooze-k8s-events` is a standalone daemon that watches the Kubernetes `core/v1` Event API and forwards interesting events to `snooze-server` as alerts. It is an **input** integration shipped as its own process (not an in-process plugin) because it maintains a long-lived watch connection rather than serving per-alert HTTP requests.

The daemon talks to the kube-apiserver over plain HTTP using only the Go standard library — it does **not** vendor `k8s.io/client-go`. It issues:

    GET {apiserver}/api/v1/events?watch=true&resourceVersion=<rv>

and streams the newline-delimited `{"type":...,"object":<Event>}` watch envelopes. Each surviving event is mapped to a `snoozetypes.Record` and posted to `POST /api/v1/alerts` via `pkg/snoozeclient`.

By default only `Warning` events are forwarded (server-side filtered with `fieldSelector=type=Warning`); `Normal` events are high-volume and rarely actionable. On a watch close or server-side timeout the daemon reconnects and resumes from the last observed `resourceVersion`; on a `410 Gone` (the resourceVersion was compacted out of etcd) it resets and restarts from the current state with a capped exponential backoff.

### What it watches

- All namespaces (`/api/v1/events`) by default, or a single namespace (`/api/v1/namespaces/<ns>/events`) when `namespace` is set.
- Event fields consumed: `metadata` (namespace, name, resourceVersion), `involvedObject` (kind, name, namespace), `reason`, `message`, `type`, `lastTimestamp`/`eventTime`, `count`, `source` (component, host) and `reportingComponent`.

## Configuration

The daemon reads `/etc/snooze/k8s-events.yaml` (override with `-c`). It auto-detects in-cluster configuration when `apiserver` is left empty.

### In-cluster vs explicit config

**In-cluster** (recommended; runs as a Deployment with a ServiceAccount): leave `apiserver` empty. The daemon then reads the projected ServiceAccount token from `/var/run/secrets/kubernetes.io/serviceaccount/token`, trusts the CA at `.../ca.crt`, and derives the apiserver address from the `KUBERNETES_SERVICE_HOST` / `KUBERNETES_SERVICE_PORT` environment variables that the kubelet injects into every pod.

**Explicit** (running outside the cluster, e.g. under systemd): set `apiserver` to the kube-apiserver URL plus a bearer token (`token_k8s` inline, or `token_file` path) and either `ca_cert` (a PEM file to trust) or `insecure_skip_verify: true`.

### RBAC

The daemon only needs read access to events cluster-wide. Create a ServiceAccount, a ClusterRole granting `get`/`list`/`watch` on `events`, and a ClusterRoleBinding:

``` yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: snooze-k8s-events
  namespace: monitoring
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: snooze-k8s-events
rules:
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["events.k8s.io"]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: snooze-k8s-events
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: snooze-k8s-events
subjects:
  - kind: ServiceAccount
    name: snooze-k8s-events
    namespace: monitoring
```

### Field reference

``` yaml
# --- Snooze server (where alerts are POSTed) ---
server: "https://snooze.example.com"
username: "ingest"
password: "hunter2"
# method: "local"          # snooze auth backend (default: local)
# token: ""                # snooze bearer token (skips username/password)
# insecure: false          # skip TLS verify for the snooze connection

# --- Kubernetes apiserver ---
# Leave apiserver empty to auto-detect in-cluster config.
apiserver: "https://10.0.0.1:6443"
token_file: "/var/run/secrets/kubernetes.io/serviceaccount/token"
ca_cert: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
# token_k8s: ""                  # inline bearer token (instead of token_file)
# insecure_skip_verify: false    # skip TLS verify for the apiserver

# --- Watch behaviour ---
namespace: ""              # "" = all namespaces
include_normal: false      # also forward Normal events (default: Warning only)
# event_types: ["Warning"] # explicit type allowlist (overrides include_normal)
resync_interval: "30m"     # recycle the watch connection periodically
dedup_window: "1m"         # suppress repeat involvedObject+reason within window (0 disables)
request_timeout: "30s"     # caps the snooze POST (not the watch stream)
debug: false

# Override / extend the reason -> severity map. Keys are Event.reason.
reasons:
  FailedScheduling: "error"
  CrashLoopBackOff: "error"
```

### Severity mapping

`Warning` events default to `warning` and `Normal` events to `info`. The following reasons are elevated out of the box (override via `reasons:`):

| `reason`                                                | severity   |
|---------------------------------------------------------|------------|
| `OOMKilling`, `Killing`, `SystemOOM`, `NodeNotReady`    | `critical` |
| `FailedScheduling`, `FailedMount`, `FailedAttachVolume` | `error`    |
| `BackOff`, `CrashLoopBackOff`, `Failed`                 | `error`    |
| `FailedCreatePodSandBox`, `Unhealthy`, `FailedKillPod`  | `error`    |

### Record mapping

| Record field | Source |
|----|----|
| `source` | constant `"kubernetes"` |
| `host` | `involvedObject.name` (falls back to `source.host`) |
| `process` | `"<involvedObject.kind>/<reason>"` |
| `severity` | reason override, else type default |
| `message` | `event.message` |
| `\`environment` | ` ``involvedObject.namespace`` (falls back to ``metadata.namespace`\`) |
| `raw` | namespace, reason, count, type, involved_object, source, reporting_component |

### Running as a Deployment (recommended)

``` yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: snooze-k8s-events
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels: { app: snooze-k8s-events }
  template:
    metadata:
      labels: { app: snooze-k8s-events }
    spec:
      serviceAccountName: snooze-k8s-events
      containers:
        - name: snooze-k8s-events
          image: snoozeweb/snooze-k8s-events:latest
          args: ["-c", "/etc/snooze/k8s-events.yaml"]
          volumeMounts:
            - { name: config, mountPath: /etc/snooze, readOnly: true }
      volumes:
        - name: config
          secret: { secretName: snooze-k8s-events-config }
```

### Running under systemd

Although it usually runs in-cluster as a Deployment, the daemon also runs as a plain systemd service when given explicit `apiserver`/`token`/`ca_cert`:

``` ini
[Unit]
Description=Snooze Kubernetes Events watcher daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-k8s-events -c /etc/snooze/k8s-events.yaml
Restart=on-failure
RestartSec=5s
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/snooze /var/log/snooze
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

## End-to-end test setup

The package ships an env-gated end-to-end test, `TestK8sEventsE2E`, that connects to a real kube-apiserver, lists a single page of events (`watch=false&limit=1`) and asserts a `200` with a parseable `EventList`. It is skipped unless the apiserver and token vars are set.

Env vars the e2e test reads:

| Variable | required | meaning |
|----|----|----|
| `SNOOZE_E2E_K8S_APISERVER` | yes | apiserver URL, e.g. `https://10.0.0.1:6443` |
| `SNOOZE_E2E_K8S_TOKEN` | yes | bearer token with `get`/`list` on events |
| `SNOOZE_E2E_K8S_CA` | no | path to the apiserver CA PEM file |
| `SNOOZE_E2E_K8S_INSECURE` | no | `true` to skip apiserver TLS verification |

A quick way to obtain a token and CA from a working `kubeconfig`:

``` console
$ export SNOOZE_E2E_K8S_APISERVER="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
$ kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d > /tmp/k8s-ca.crt
$ export SNOOZE_E2E_K8S_CA=/tmp/k8s-ca.crt
$ export SNOOZE_E2E_K8S_TOKEN="$(kubectl create token snooze-k8s-events -n monitoring)"
$ go test -run E2E ./internal/components/k8sevents/...
```

## Notes & limitations

- **Warning-only by default.** `Normal` events are not forwarded unless `include_normal: true` (or an explicit `event_types` list). The Warning-only path also pushes the filter server-side via `fieldSelector=type=Warning` to spare both ends the noise.
- **De-duplication is best-effort and in-memory.** Repeated `involvedObject`+`reason` events inside `dedup_window` are suppressed. The window state is per-process and is lost across restarts; rely on Snooze aggregate rules for durable de-duplication.
- **resourceVersion is opaque.** On a `410 Gone` the watch restarts from the current cluster state, so events that occurred during the gap (compaction window) are not replayed.
- **No leader election.** Run a single replica. Two replicas will each forward every event (Snooze aggregate rules will coalesce them, but it doubles load).
- **Events are ephemeral.** Kubernetes garbage-collects events after ~1h by default; the daemon forwards them as they stream, it does not backfill history on startup beyond what the apiserver still holds.

