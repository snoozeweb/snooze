---
sidebar_position: 0.5
---

# Send your first alert

New Snooze install with an empty Alerts page? This quickstart shows the fastest
ways to get alerts flowing in. Snooze accepts alerts three ways:

- the **REST API** — the universal "just POST JSON" endpoint,
- **webhook receivers** — built-in HTTP endpoints that speak a specific tool's
  format (Grafana, Alertmanager, Datadog, …),
- **daemon inputs** — separate listeners for non-HTTP protocols (syslog, SNMP
  traps, RELP, email, OTLP, Kubernetes events).

Replace `https://snooze.example.com` below with your server's address.

## 1. REST API (works everywhere)

`POST /api/v1/alerts` accepts a single JSON object or an array. No plugin
required. It is **public by default** — restrict it with a reverse proxy or an
[ingest token](./index.md#authenticating-ingest).

``` console
$ curl -s -X POST https://snooze.example.com/api/v1/alerts \
    -H 'Content-Type: application/json' \
    -d '{"host":"web-1","severity":"err","message":"Disk usage exceeded 90% on /var"}'
```

You should see the alert appear on the Alerts page within a few seconds. Full
reference, the `snooze` CLI, and the Python client: [REST API](./rest-api.md).

## 2. Webhook receivers

Each receiver is auto-mounted at `POST /api/v1/webhook/<name>` — point your
tool's webhook/notification at that URL. Receivers are unauthenticated by
default; see [Authenticating ingest](./index.md#authenticating-ingest) to add a
shared token or per-source signature checks.

| Tool | Endpoint | Setup |
|------|----------|-------|
| Grafana | `/api/v1/webhook/grafana` | [Grafana](./grafana.md) |
| Alertmanager | `/api/v1/webhook/alertmanager` | [Alertmanager](./alertmanager.md) |
| Prometheus | `/api/v1/webhook/prometheus` | [Prometheus](./prometheus.md) |
| Datadog | `/api/v1/webhook/datadog` | [Datadog](./datadog.md) |
| CloudWatch (SNS) | `/api/v1/webhook/cloudwatch` | [CloudWatch](./cloudwatch.md) |
| Sentry | `/api/v1/webhook/sentry` | [Sentry](./sentry.md) |
| New Relic | `/api/v1/webhook/newrelic` | [New Relic](./newrelic.md) |
| Azure Monitor | `/api/v1/webhook/azuremonitor` | [Azure Monitor](./azuremonitor.md) |
| InfluxDB 2 | `/api/v1/webhook/influxdb2` | [InfluxDB 2](./influxdb2.md) |
| Kapacitor | `/api/v1/webhook/kapacitor` | [Kapacitor](./kapacitor.md) |
| Heartbeat | `/api/v1/webhook/heartbeat` | [Heartbeat](./heartbeat.md) |

## 3. Daemon inputs

These run as their own binaries/listeners alongside the server.

| Input | Listens on | Setup |
|-------|-----------|-------|
| Syslog | UDP `:514` / TCP `:6514` | [Syslog](./syslog.md) |
| SNMP trap | UDP `:162` | [SNMP trap](./snmptrap.md) |
| RELP | TCP `:2514` | [RELP](./relp.md) |
| SMTP (email) | TCP `:25` | [SMTP](./smtp.md) |
| OTLP logs | HTTP `:4318` | [OTLP](./otlp.md) |
| Kubernetes events | in-cluster watch | [Kubernetes events](./k8s-events.md) |

## Next steps

Once alerts start arriving, open the [Manage alerts](../alerts.md) page to triage
them — acknowledge, snooze, re-escalate, and comment from the web UI.
