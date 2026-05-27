.. _integration-newrelic:

========================
New Relic (input)
========================

Overview
========

The ``newrelic`` plugin is an in-process WebhookReceiver that exposes an
inbound HTTP endpoint at ``/api/v1/webhook/newrelic``. It accepts alerts from
New Relic in two payload shapes:

* **Workflow / Notification webhook** (recommended) — produced by New Relic's
  *Destinations* → *Webhook* channel inside a *Workflow*. The operator fully
  controls the JSON body via New Relic's template variables.
* **Legacy default condition webhook** — produced by the classic New Relic
  *Alerts* → *Notification channels* → *Webhook* feature. No template
  customisation is needed; New Relic sends a fixed JSON schema.

Each received payload is mapped to a ``snoozetypes.Record`` and submitted to
the Snooze processing pipeline.

Configuration
=============

Inbound URL
-----------

Register the following URL as the webhook destination in New Relic:

.. code-block:: text

    https://<your-snooze-server>/api/v1/webhook/newrelic

No credentials are required. The endpoint accepts unauthenticated POST
requests (matching the ``route_defaults`` in the plugin's metadata).

Workflow webhook — recommended template
----------------------------------------

When creating a **Workflow** in New Relic (*Alerts & AI → Workflows*), add a
*Webhook* destination and paste the following JSON body template.  All
``{{ }}`` expressions are New Relic notification template variables.

.. code-block:: json

    {
      "id": "{{ issueId }}",
      "issueUrl": "{{ issuePageUrl }}",
      "title": "{{ issueTitle }}",
      "priority": "{{ priority }}",
      "state": "{{ state }}",
      "trigger": "{{ triggerEvent }}",
      "timestamp": {{ updatedAt }},
      "accountName": "{{ accumulations.conditionProduct.[0] }}",
      "totalIncidents": {{ totalIncidents }},
      "owner": "{{ owner }}",
      "impactedEntities": {{ impactedEntitiesCount }},
      "labels": {
        "policy": "{{ accumulations.policyName.[0] }}",
        "condition": "{{ accumulations.conditionName.[0] }}"
      }
    }

.. note::

   The ``impactedEntities`` field shown above maps ``impactedEntitiesCount``
   (a number). For host extraction from the entity name, use the richer
   notification payload option and replace the value with
   ``"{{ entitiesData.names }}"`` if your New Relic plan exposes it.
   The plugin falls back gracefully to the ``title`` field if
   ``impactedEntities`` is absent or empty.

Severity mapping
----------------

New Relic ``priority`` / ``severity`` values are mapped as follows:

======== ==========
New Relic Snooze
======== ==========
CRITICAL critical
HIGH     error
MEDIUM   warning
LOW      info
(other)  critical
======== ==========

A ``state: CLOSED`` (workflow) or ``current_state: closed`` (legacy) record is
emitted with ``State: "close"`` so downstream Snooze processors can resolve the
matching open alert. When the closing record has ``critical`` severity it is
downgraded to ``info``.

Field reference
---------------

.. code-block:: text
   :caption: Record fields set by the newrelic plugin

   Source:   "newrelic"
   Host:     impactedEntities[0].name  (workflow)
             targets[0].name           (legacy)
             title / condition_name    (fallback)
   Severity: mapped from priority / severity (see table above)
   Message:  title (workflow)
             condition_name + ": " + details (legacy)
   State:    "close" when CLOSED / closed; empty otherwise
   Raw:      issueUrl, priority, state, accountName, labels  (workflow)
             incident_url, severity, current_state, account_name  (legacy)

curl example
------------

.. code-block:: console

    $ curl -s -X POST https://<your-snooze-server>/api/v1/webhook/newrelic \
        -H 'Content-Type: application/json' \
        -d '{
          "id": "test-001",
          "issueUrl": "https://one.newrelic.com/alerts-ai/issues/test-001",
          "title": "High error rate on payment-service",
          "priority": "CRITICAL",
          "state": "ACTIVATED",
          "trigger": "INCIDENT_ADDED",
          "timestamp": 1716800000000,
          "accountName": "Acme Corp",
          "totalIncidents": 1,
          "owner": "team-backend",
          "impactedEntities": ["payment-service"],
          "labels": {"env": "production"}
        }'
    {"accepted":1,"received":1,"status":"ok"}

End-to-end test setup
=====================

The package ships an env-gated end-to-end test in ``e2e_test.go``.  It POSTs a
realistic workflow webhook body to a live Snooze instance and asserts a 2xx
response.

**Required environment variable:**

.. list-table::
   :header-rows: 1

   * - Variable
     - Description
   * - ``SNOOZE_E2E_NEWRELIC_URL``
     - Full URL of the running snooze-server's New Relic webhook endpoint,
       e.g. ``https://snooze.example.com/api/v1/webhook/newrelic``.

.. code-block:: console

    $ export SNOOZE_E2E_NEWRELIC_URL="https://snooze.example.com/api/v1/webhook/newrelic"
    $ go test -run E2E ./internal/pluginimpl/newrelic/...

Notes & limitations
===================

* **gRPC / New Relic agent streams not supported.** The plugin only handles
  inbound webhook POST requests. Streaming telemetry (metrics, traces, logs)
  from the New Relic agent protocol requires a separate integration.
* **Workflow template variables** vary between New Relic accounts and plan
  tiers. Validate the template in the New Relic *Workflows* → *Test
  notification* panel before going live.
* **Legacy shape detection.** The plugin auto-detects the payload shape at
  runtime: if the JSON object contains a ``state`` key (uppercased) together
  with a ``title`` key, or an ``issueUrl`` key, it is treated as a workflow
  payload; otherwise the legacy schema is assumed. This heuristic covers all
  known New Relic payload variants.
* **No signature verification.** New Relic webhooks do not carry an HMAC
  signature by default. If your threat model requires verification, place the
  endpoint behind a reverse proxy that enforces an IP allowlist or shared
  secret header.
