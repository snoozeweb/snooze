---
sidebar_position: 19
---

# Saved searches

## Overview

**Saved searches** let operators bookmark a named alert filter — a human label
paired with a [query-language](./querylanguage.md) condition string — so that
recurring triage queries like "prod criticals unacked" survive page reloads and
roam across browsers and devices. They are stored server-side rather than in the
browser, so a saved search follows you to any machine you log into.

Each saved search records:

- **Name** — the human label shown in the panel, e.g. `prod criticals unacked`.
- **Query** — the raw DSL string applied to the search bar, e.g.
  `severity = critical AND state = open`.

## Scope and ownership

Saved searches are scoped to the **current tenant** and the **signed-in
operator**. The `owner` and `tenant_id` are stamped server-side from your
session, so:

- You only see (and apply) your own saved searches.
- Two operators — or two tenants — can each keep a bookmark called
  `prod criticals` without colliding.
- You cannot create, edit, or delete another operator's saved search.
  Administrators may delete any operator's entry to curate stale bookmarks.

A saved search name must be unique per operator: saving a second search under a
name you already use is rejected rather than silently overwriting the first.

## Using saved searches from the Alerts page

The **Saved searches** panel is a collapsible section near the top of the
[Alerts](./alerts.md) page, just below the lifecycle tabs and environment bar.

### Save a search

1. Type or build a query in the search bar.
2. Expand the **Saved searches** panel.
3. With a query present, an inline **Save current search** form appears. Enter a
   name and click **Save** (an empty name is rejected).

### Apply a search

Click any entry in the panel. Its stored query is loaded into the search bar and
the alert list is refetched with that filter applied — exactly as if you had
typed the query yourself.

### Delete a search

Click the trash icon on a saved-search row to remove it.

## API

Saved searches use the generic CRUD surface under `/api/v1/savedsearch`
(see the [API reference](pathname:///api/)). The `owner` field is always
stamped from the authenticated subject, so any client-supplied `owner` is
ignored.

- `GET /api/v1/savedsearch` — list your saved searches.
- `POST /api/v1/savedsearch` with `{ "name": "...", "query": "..." }` — create one.
- `DELETE /api/v1/savedsearch/{uid}` — delete one.
