// Static catalogue powering the "How to inject alerts" dialog
// (InjectAlertsDialog). The endpoint paths are stable, but the snippets and
// prose are editorial and can't be derived from the backend — so they live
// here rather than behind an API. Keep in sync with the per-integration pages
// under docs/content/general/integrations/.

export type InjectionFamily = "rest" | "webhook" | "daemon";

export type InjectionSource = {
  /** Stable id; also the picker key. */
  id: string;
  /** Display name. */
  name: string;
  family: InjectionFamily;
  /** HTTP "METHOD /path" for HTTP sources; omitted for daemon listeners. */
  endpoint?: string;
  /** One-line "what it is / how to set it up". */
  summary: string;
  /** Copy-pasteable snippet. `baseUrl` is the live server origin. */
  snippet: (baseUrl: string) => string;
  /** Route slug of the existing per-integration docs page. */
  docSlug: string;
};

/**
 * hostOf extracts a bare hostname from an origin, for daemon examples that
 * bind their own ports rather than the HTTP origin. Falls back to the input
 * when it is not a parseable URL.
 */
export function hostOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname;
  } catch {
    return baseUrl;
  }
}

export const REST_SOURCE: InjectionSource = {
  id: "rest",
  name: "REST API",
  family: "rest",
  endpoint: "POST /api/v1/alerts",
  summary:
    "The universal ingest endpoint — any tool that can POST JSON over HTTP can send alerts, no plugin required. Public by default; restrict it with a reverse proxy or an ingest token.",
  snippet: (baseUrl) =>
    `curl -s -X POST ${baseUrl}/api/v1/alerts \\
  -H 'Content-Type: application/json' \\
  -d '{"host":"web-1","severity":"err","message":"Disk usage exceeded 90% on /var"}'`,
  docSlug: "general/integrations/rest-api",
};

// Webhook seeds carry everything except the `family`/`snippet` fields that are
// identical across all of them; the .map below attaches those. Typing the
// seed array explicitly (with a required `endpoint`) means a seed that forgets
// `endpoint` is a compile error rather than a runtime crash.
type WebhookSeed = Omit<InjectionSource, "family" | "snippet"> & { endpoint: string };

/** Extract the path from an "METHOD /path" endpoint string. */
const webhookPath = (endpoint: string): string => endpoint.replace(/^POST /, "");

const WEBHOOK_SEEDS: WebhookSeed[] = [
  {
    id: "grafana",
    name: "Grafana",
    endpoint: "POST /api/v1/webhook/grafana",
    summary:
      "Add a Grafana webhook contact point (or legacy webhook notifier) pointing at this URL. Each evaluated alert match becomes a record.",
    docSlug: "general/integrations/grafana",
  },
  {
    id: "alertmanager",
    name: "Alertmanager",
    endpoint: "POST /api/v1/webhook/alertmanager",
    summary:
      "Add a Prometheus Alertmanager `webhook_configs` receiver targeting this URL. Firing and resolved alerts become records.",
    docSlug: "general/integrations/alertmanager",
  },
  {
    id: "prometheus",
    name: "Prometheus",
    endpoint: "POST /api/v1/webhook/prometheus",
    summary:
      "Forward Prometheus alerting webhooks to this URL when you want Snooze to receive them directly.",
    docSlug: "general/integrations/prometheus",
  },
  {
    id: "datadog",
    name: "Datadog",
    endpoint: "POST /api/v1/webhook/datadog",
    summary:
      "Create a Datadog Webhooks integration with this URL as the endpoint, then call it from a monitor's notification message.",
    docSlug: "general/integrations/datadog",
  },
  {
    id: "cloudwatch",
    name: "CloudWatch (SNS)",
    endpoint: "POST /api/v1/webhook/cloudwatch",
    summary:
      "Subscribe this URL to the SNS topic that receives your CloudWatch alarm notifications. Set `ingest.sns_verify: true` to verify SNS signatures.",
    docSlug: "general/integrations/cloudwatch",
  },
  {
    id: "sentry",
    name: "Sentry",
    endpoint: "POST /api/v1/webhook/sentry",
    summary:
      "Add this URL as a Sentry webhook (internal integration / alert rule). Set `ingest.sentry_secret` to verify the HMAC signature.",
    docSlug: "general/integrations/sentry",
  },
  {
    id: "newrelic",
    name: "New Relic",
    endpoint: "POST /api/v1/webhook/newrelic",
    summary: "Point a New Relic workflow webhook notification destination at this URL.",
    docSlug: "general/integrations/newrelic",
  },
  {
    id: "azuremonitor",
    name: "Azure Monitor",
    endpoint: "POST /api/v1/webhook/azuremonitor",
    summary: "Add an Azure Monitor action group webhook action targeting this URL.",
    docSlug: "general/integrations/azuremonitor",
  },
  {
    id: "influxdb2",
    name: "InfluxDB 2",
    endpoint: "POST /api/v1/webhook/influxdb2",
    summary: "Configure an InfluxDB 2 HTTP notification endpoint pointing at this URL.",
    docSlug: "general/integrations/influxdb2",
  },
  {
    id: "kapacitor",
    name: "Kapacitor",
    endpoint: "POST /api/v1/webhook/kapacitor",
    summary: "Add a Kapacitor HTTP POST handler (or `.post()` in a TICKscript) targeting this URL.",
    docSlug: "general/integrations/kapacitor",
  },
  {
    id: "heartbeat",
    name: "Heartbeat",
    endpoint: "POST /api/v1/webhook/heartbeat",
    summary:
      "Dead-man's switch: have a cron job periodically hit this URL. A missed ping escalates into an alert. The URL carries a per-heartbeat token (see docs).",
    docSlug: "general/integrations/heartbeat",
  },
];

export const WEBHOOK_SOURCES: InjectionSource[] = WEBHOOK_SEEDS.map((s) => ({
  ...s,
  family: "webhook" as const,
  snippet: (baseUrl: string) => `${baseUrl}${webhookPath(s.endpoint)}`,
}));

export const DAEMON_SOURCES: InjectionSource[] = [
  {
    id: "syslog",
    name: "Syslog",
    family: "daemon",
    summary:
      "Run the snooze-syslog daemon (UDP :514 / TCP :6514) and forward your hosts' syslog to it. Each log line maps to a record.",
    snippet: (baseUrl) =>
      `# /etc/rsyslog.d/snooze.conf — forward everything over UDP\n*.*  @${hostOf(baseUrl)}:514`,
    docSlug: "general/integrations/syslog",
  },
  {
    id: "snmptrap",
    name: "SNMP trap",
    family: "daemon",
    summary: "Run the snooze-snmptrap daemon (UDP :162) and point your devices' trap sink at it.",
    snippet: (baseUrl) =>
      `snmptrap -v2c -c public ${hostOf(baseUrl)}:162 '' \\\n  1.3.6.1.4.1.8072.2.3.0.1`,
    docSlug: "general/integrations/snmptrap",
  },
  {
    id: "relp",
    name: "RELP",
    family: "daemon",
    summary:
      "Run the snooze-relp daemon (TCP :2514) and forward syslog over RELP for guaranteed delivery.",
    snippet: (baseUrl) =>
      `# /etc/rsyslog.d/snooze-relp.conf\nmodule(load="omrelp")\naction(type="omrelp" target="${hostOf(baseUrl)}" port="2514")`,
    docSlug: "general/integrations/relp",
  },
  {
    id: "smtp",
    name: "SMTP (email)",
    family: "daemon",
    summary:
      "Run the snooze-smtp daemon (TCP :25) and have monitoring tools email their alerts to it.",
    snippet: (baseUrl) =>
      `swaks --server ${hostOf(baseUrl)}:25 --to alerts@snooze \\\n  --from monitor@host --header "Subject: Disk full on web-1" --body "..."`,
    docSlug: "general/integrations/smtp",
  },
  {
    id: "otlp",
    name: "OTLP logs",
    family: "daemon",
    summary:
      "Run the snooze-otlp daemon (OTLP/HTTP on :4318) and point your OpenTelemetry log exporter at it.",
    snippet: (baseUrl) =>
      `export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://${hostOf(baseUrl)}:4318/v1/logs`,
    docSlug: "general/integrations/otlp",
  },
  {
    id: "k8s-events",
    name: "Kubernetes events",
    family: "daemon",
    summary:
      "Deploy snooze-k8s-events in your cluster; it watches the Kubernetes Event API and turns events into records. No inbound endpoint to configure.",
    snippet: () =>
      `# Runs in-cluster as a Deployment; no client config.\n# See the docs for the manifest / Helm values.`,
    docSlug: "general/integrations/k8s-events",
  },
];

export const INJECTION_SOURCES: InjectionSource[] = [
  REST_SOURCE,
  ...WEBHOOK_SOURCES,
  ...DAEMON_SOURCES,
];

export function sourcesForFamily(family: InjectionFamily): InjectionSource[] {
  return INJECTION_SOURCES.filter((s) => s.family === family);
}
