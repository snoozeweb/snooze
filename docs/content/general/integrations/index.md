---
sidebar_position: 0
---

# Integrations

Snooze ships a broad catalogue of **input** (ingest) and **output** (notification) integrations. Inputs map a foreign alert source onto a Snooze record; outputs deliver matching alerts to an external destination.

Each page documents the integration's configuration surface and how to verify it. The newer plugin-based integrations also ship an env-gated end-to-end test (see each page's "End-to-end test setup" section); run them all with:

``` console
$ task go:test:e2e        # or: go test -run E2E ./...
```

Every such test self-skips unless its `SNOOZE_E2E_*` credentials are exported, so the suite stays green with no external dependencies.

## Authenticating ingest

Inbound webhook receivers (`/api/v1/webhook/*`) are **unauthenticated by default**, matching historical behaviour — anyone who can reach the endpoint can submit alerts. Network isolation (a reverse proxy or a restricted monitoring network) is the recommended baseline. Three opt-in layers harden ingest on top of it, configured in the bootstrap `ingest` section:

- **Shared ingest token** — set `ingest.token` to require every webhook request to carry it as `Authorization: Bearer <token>` or `?token=<token>`. Applies to all receivers at once.
- **Per-source signatures** — set `ingest.sns_verify: true` to verify Amazon SNS message signatures on the *cloudwatch* receiver, and/or `ingest.sentry_secret` to verify the Sentry `sentry-hook-signature` HMAC on the *sentry* receiver.
- **Per-heartbeat token** — the *heartbeat* ping carries an unguessable per-heartbeat token in its URL, while its CRUD collection uses normal operator authentication. The shared ingest token (if set) stacks on top.

``` yaml
ingest:
  token: "<random shared secret>"          # require on all webhook receivers
  sns_verify: true                         # verify SNS signatures (cloudwatch)
  sentry_secret: "<sentry client secret>"  # verify Sentry HMAC (sentry)
```

Plugin **CRUD** endpoints (rules, snoozes, notifications, heartbeats, …) are unaffected — they always require a logged-in operator (JWT), independent of these ingest knobs.

## Inputs

New here? Start with **[Send your first alert](./sending-alerts.md)** for the
fastest paths to get alerts flowing, then see the per-integration pages below.

**Generic entry points**

- [REST API](./rest-api.md) — direct HTTP `POST /api/v1/alerts`; the base for
  all integrations
- [Custom source mapping](./custom-source.md) — normalize arbitrary JSON
  payloads from any source without writing code

**Webhook receivers** (mounted at `/api/v1/webhook/*`)

- [Alertmanager](./alertmanager.md)
- [Azure Monitor](./azuremonitor.md)
- [CloudWatch / SNS](./cloudwatch.md)
- [Datadog](./datadog.md)
- [Grafana](./grafana.md)
- [Graylog](./graylog.md)
- [InfluxDB 2 / Kapacitor](./influxdb2.md) · [Kapacitor](./kapacitor.md)
- [New Relic](./newrelic.md)
- [OpenTelemetry (OTLP)](./otlp.md)
- [PagerDuty inbound sync](./pagerduty.md#inbound-webhook-status-sync)
- [Pingdom](./pingdom.md)
- [Prometheus](./prometheus.md)
- [Sentry](./sentry.md)
- [Stackdriver / Google Cloud Monitoring](./stackdriver.md)

**Standalone collector daemons**

- [Heartbeat](./heartbeat.md) — dead-man's-switch monitoring
- [Kubernetes events](./k8s-events.md)
- [Pacemaker](./pacemaker.md)
- [RELP](./relp.md) — syslog RELP receiver
- [SMTP](./smtp.md) — receive alerts as email
- [SNMP traps](./snmptrap.md)
- [Syslog](./syslog.md) (legacy daemon)

## Outputs

- [Discord](./discord.md)
- [Google Chat](./googlechat.md)
- [Jira](./jira.md)
- [Mail (SMTP)](./mail.md)
- [Mattermost](./mattermost.md)
- [MCP](./mcp.md)
- [Microsoft Teams](./teams.md)
- [Ntfy](./ntfy.md)
- [OpsGenie](./opsgenie.md)
- [PagerDuty](./pagerduty.md)
- [Patlite](./patlite.md)
- [Pushover](./pushover.md)
- [Script](./script.md)
- [ServiceNow](./servicenow.md)
- [Slack](./slack.md) (interactive ack/close/re-open buttons supported)
- [Snooze peer (federation)](./snoozepeer.md) — relay alerts to a downstream Snooze/HTTP peer
- [SNS](./sns.md)
- [Statuspage](./statuspage.md)
- [Telegram](./telegram.md) (interactive ack/close/re-open buttons supported)
- [Twilio](./twilio.md)
- [Webhook (generic)](./webhook.md)
