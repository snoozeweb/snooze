# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Two audiences, triage-first:

- **On-call ops/SRE engineers** triaging live alerts under time pressure. The alerts table is the home surface and the workflow the product is judged on.
- **Monitoring-platform admins** who configure aggregate rules, rules, snoozes, escalations, and notifiers. Secondary but constant: pipeline configuration is done in the same web UI.

A reference deployment runs internally at Egerie (multitenant, Entra SSO), but the product is open source and designed for general self-hosted operators.

## Product Purpose

Snooze is a clustered log-aggregation and alerting backend with a web UI. It ingests events from many sources (syslog, RELP, SNMP traps, SMTP, webhooks, Grafana, AlertManager), runs them through a pipeline of transforms, aggregations, rules, and snoozes, and routes survivors to chat, email, ticketing (Jira, ServiceNow), status pages, or arbitrary webhooks. Success means the on-call person receives fewer, better notifications and can triage everything remaining from one screen.

## Positioning

**Noise reduction is the core promise.** Snoozes, throttling, anti-flapping budgets, aggregation windows, and escalation-aware re-notification are first-class, user-configurable objects — not YAML shipped with the binary. Neighboring products (Alerta, AlertManager, Opsgenie) treat de-duplication or silencing as a feature; Snooze treats "fewer, better notifications" as the product.

## Operating Context

- Operators self-host: Docker, docker-compose (Mongo replica set / Postgres / SQLite profiles), RPM/DEB packages with systemd units, or Helm. Multi-node clustering with live config reload is a core mode.
- The web UI (React 19 + Vite, TypeScript, Radix primitives, TanStack Query, served by the Go binary) is where both triage and pipeline configuration happen. Default login flow includes local users plus OIDC SSO.
- On-call use often happens in dark rooms and on shared NOC screens; admins work in long configuration sessions (rule editors, condition builders, notifier forms).
- Public demo at https://try.snoozeweb.net; docs site generated from `docs/`.

## Capabilities and Constraints

- Backend: Go, one clustered binary (`cmd/snooze-server`) plus auxiliary input daemons (syslog, snmptrap) and output daemons (jira, teams). Storage: MongoDB, Postgres, or SQLite.
- Multitenant (shared schema + tenant_id); web UI is tenant-aware.
- ~19 notifier types with escalation-aware re-notification; webhook input/output; API-first (OpenAPI spec at `api/openapi.yaml`, web types generated from it).
- Web e2e suite (Playwright) runs against a real built server; keep it green — it is the regression gate for UI work.
- Terminology is fixed and load-bearing: *record* (raw event), *alert/aggregate*, *rule*, *aggregate rule*, *snooze* (time-boxed silencing filter), *escalation*, *notification*, *action/notifier*, *severity*, *ack/close/re-open*.

## Brand Commitments

- The **Snooze name and logo** (`docs/content/images/logo.png`) are binding. The visual theme (current IBM Plex + palette) may evolve freely.
- **Dark-mode parity is binding:** every surface must be first-class in both light and dark themes.
- No other formal brand or accessibility commitments were established; good practice applies.

## Evidence on Hand

- Real screenshots in `docs/content/images/` (web UI, alerts table) used by README and docs.
- Live demo instance (try.snoozeweb.net) and an internal production deployment exist as demonstrations.
- No testimonials, case studies, or benchmark claims exist — do not fabricate any.

## Product Principles

1. **Triage speed first.** The on-call path (see → understand → ack/close/snooze) outranks configuration ergonomics whenever they compete.
2. **Fewer, better notifications.** Any feature or surface should make noise reduction more legible: show why something fired, why it was suppressed, and what will fire next.
3. **Configuration is UI, not YAML.** Pipeline objects are edited in the web interface by admins; forms and condition builders must be trustworthy enough to replace config files.
4. **Self-hosted and boring to operate.** One binary, standard packaging, live reload; nothing in the product may assume a SaaS control plane.
5. **Both themes, all states.** Dark and light are equal citizens; empty, error, and degraded-cluster states are part of every surface.
