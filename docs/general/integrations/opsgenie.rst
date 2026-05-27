.. _integration-opsgenie:

======================
Opsgenie (output)
======================

Overview
========

The **opsgenie** plugin is an in-process Notifier that creates or closes
`Opsgenie <https://www.atlassian.com/software/opsgenie>`_ alerts via the
`Alert API v2 <https://docs.opsgenie.com/docs/alert-api>`_.

When a notification rule matches a firing record, the plugin POSTs a
create-alert request to Opsgenie. When the record's ``state`` field is
``"close"``, a close-alert request is sent instead so the corresponding
Opsgenie alert is resolved automatically.

Alert deduplication and close correlation are anchored on the ``alias``
field, which is set to ``rec.Hash`` (falling back to ``rec.UID``). Because
the same alias is used for both create and close, Opsgenie will deduplicate
repeated fires and correctly resolve the alert on the first close event.

The plugin uses ``net/http`` only — no Opsgenie SDK or other external
library is required.

.. note::

   Atlassian has announced end-of-life for standalone Opsgenie
   (targeted around 2027). New deployments on Atlassian Cloud are directed
   to **Atlassian Operations** (formerly OpsGenie). The Alert API v2
   endpoints used by this plugin are compatible with both the legacy
   Opsgenie product and the migrated service, so no configuration change
   is expected during the migration window.

Configuration
=============

Wire the plugin through a **Notification → Action** in the Snooze UI or
configuration file. Set the action type to ``opsgenie`` and fill the
``action_form`` fields described below.

Field reference
---------------

.. list-table::
   :widths: 20 15 65
   :header-rows: 1

   * - Field
     - Default
     - Description
   * - ``api_key``
     - *(required)*
     - Opsgenie API Integration key. Create an API Integration in Opsgenie
       (see `End-to-end test setup`_) and copy the API key. Stored as a
       ``Password`` field — not shown in the UI after saving.
   * - ``region``
     - ``us``
     - Opsgenie region. ``us`` uses ``https://api.opsgenie.com``;
       ``eu`` uses ``https://api.eu.opsgenie.com``. Ignored when
       ``api_base`` is set.
   * - ``priority``
     - ``auto``
     - Alert priority. ``auto`` maps the record severity to a Opsgenie
       priority (see `Severity to priority mapping`_). Can be overridden to
       a fixed value: ``P1``, ``P2``, ``P3``, ``P4``, or ``P5``.
   * - ``source``
     - ``Snooze``
     - The ``source`` field sent to Opsgenie. Visible in the alert detail
       view.
   * - ``tags``
     - *(empty)*
     - Optional comma-separated list of tags to attach to the alert,
       e.g. ``prod,snooze,db``.
   * - ``api_base``
     - *(derived from region)*
     - Override the full Opsgenie API base URL, e.g. for an on-premises
       proxy or a non-standard endpoint. When set, ``region`` is ignored.
       Trailing slashes are stripped automatically.
   * - ``timeout``
     - ``10s``
     - HTTP request timeout as a Go duration string (e.g. ``5s``, ``30s``).

.. code-block:: yaml
   :caption: Example action_form values

   api_key: "REPLACE_WITH_YOUR_GENIEKEY"
   region: "eu"
   priority: "auto"
   source: "Snooze"
   tags: "prod,snooze"
   timeout: "10s"

Severity to priority mapping
-----------------------------

When ``priority`` is set to ``auto`` (the default), the record's
``severity`` field is mapped to an Opsgenie priority as follows:

.. list-table::
   :widths: 40 20 40
   :header-rows: 1

   * - Severity
     - Priority
     - Description
   * - ``emergency``, ``critical``
     - P1 – Critical
     - Service is down or major impact.
   * - ``error``, ``err``
     - P2 – High
     - Service degraded, action required.
   * - ``warning``
     - P3 – Moderate
     - Potential issue, investigate soon.
   * - ``notice``
     - P4 – Low
     - Informational, no immediate action.
   * - ``info``, ``debug``, *(unknown)*
     - P5 – Informational
     - Lowest severity, for visibility only.

End-to-end test setup
=====================

To run the end-to-end test you need an Opsgenie API Integration key.

1. In Opsgenie (or Atlassian Operations), open **Settings** →
   **Integrations** → **Add integration**.
2. Search for **API** and click **Add**.
3. Give the integration a name (e.g. ``snooze-e2e``), note your region
   (US or EU), and click **Save integration**.
4. Copy the **API key** shown on the integration detail page.
5. Export the environment variables and run the test:

.. code-block:: console

   $ export SNOOZE_E2E_OPSGENIE_API_KEY="your-geniekey-here"
   # Optional — defaults to "us":
   $ export SNOOZE_E2E_OPSGENIE_REGION="eu"

   $ go test -run TestOpsgenieE2E ./internal/pluginimpl/opsgenie/...

The test creates one alert and then closes it. Both calls must succeed
(HTTP 202) for the test to pass. The alert should appear briefly in your
Opsgenie alert list and transition to **Closed** within a few seconds.

**Environment variables read by the e2e test:**

.. list-table::
   :widths: 45 55
   :header-rows: 1

   * - Variable
     - Purpose
   * - ``SNOOZE_E2E_OPSGENIE_API_KEY``
     - Opsgenie API Integration key (GenieKey). The test is skipped when
       this variable is unset.
   * - ``SNOOZE_E2E_OPSGENIE_REGION``
     - Region selector: ``us`` (default) or ``eu``. Optional.

Notes & limitations
===================

- Only the **Alert API** (create / close) is implemented. Escalation
  policies, schedules, and on-call management are not accessible through
  this plugin.
- The ``alias`` field (``rec.Hash`` or ``rec.UID``) must be unique enough
  for your deduplication needs. Opsgenie limits aliases to 512 UTF-8
  characters and certain special characters may be percent-encoded by
  Opsgenie internally.
- Opsgenie's Alert API enforces a rate limit (by default 60 requests per
  minute per integration key). The plugin does not implement client-side
  rate limiting or automatic retries; the notification worker is responsible
  for retry and dead-letter handling.
- The ``message`` field sent to Opsgenie is the raw ``rec.Message`` value.
  Opsgenie limits ``message`` to 130 characters; longer values will be
  silently truncated by Opsgenie.
- gRPC / Opsgenie Heartbeat endpoints are not supported.
- Atlassian is sunsetting standalone Opsgenie around 2027 in favour of
  Atlassian Operations. During the migration window both the legacy
  ``api.opsgenie.com`` and the migrated endpoints share the same Alert API
  v2 contract, so no Snooze configuration change is anticipated.
