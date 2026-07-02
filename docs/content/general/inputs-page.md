---
sidebar_position: 4.5
---

# Inputs page

The **Inputs** page (Admin → Inputs, at `/web/admin/inputs`) lists every alert
input Snooze supports and shows, for each, when it last delivered an alert.

## What it shows

The table has one row per supported input, with the following columns:

- **Input** — the input's display name.
- **Type** — its family: `REST`, `Webhook`, or `Daemon`.
- **Last received** — the most recent time an alert arrived from that input,
  within a rolling ~30-day window. Catalogued inputs that haven't sent in the
  window show **never**. This is derived from the `source` stamped on stored
  alerts, so it reflects real ingestion, not configuration.
- **Docs** — a link to that input's integration page (shown when one exists).
- **Setup** — opens the _How to inject alerts_ guide focused on that input.

The REST input shows **—** for "last received": direct `POST /api/v1/alerts`
callers set their own arbitrary `source`, so activity can't be attributed to a
single input.

## Other sources

Any `source` seen in records that doesn't match a known input — a custom REST
poster, or a receiver without a catalogue entry — is listed under **Other
sources** so no ingestion goes unnoticed. This section only appears when at
least one such source exists.

## How it works

The page reads `GET /api/v1/inputs`, which returns, per source, the most recent
alert epoch (`last_epoch`) and a record `count` within the window. The lookback
defaults to ~30 days and can be overridden per request with a `?since=<unix
epoch seconds>` query parameter. The endpoint requires the `ro_stats` (or
`rw_stats`) permission — the same permission that gates the Inputs nav entry.

The list of supported inputs is the same catalogue that powers the _How to
inject alerts_ guide (opened from the page header via **How to send alerts**),
so the two never drift.
