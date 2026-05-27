.. _integration-statuspage:

====================================
Atlassian Statuspage (output)
====================================

Overview
========

The **Statuspage** notifier is an in-process output plugin that creates and
resolves `Atlassian Statuspage <https://www.atlassian.com/software/statuspage>`_
incidents in response to Snooze alerts.

When a matching alert fires (``rec.State`` is empty) the plugin calls the
Statuspage REST API v1 to **create** a new incident.  When the same alert
closes (``rec.State == "close"``) the plugin fetches the list of unresolved
incidents and **resolves** the most-recently-created incident whose name
matches the rendered name template.

The plugin uses ``net/http`` only; no Statuspage SDK is required.

Configuration
=============

Configure the plugin as a Snooze **Action** of type *Post to Atlassian
Statuspage*.  All knobs are supplied through the ``action_form``:

.. list-table::
   :header-rows: 1
   :widths: 20 10 70

   * - Field
     - Required
     - Description
   * - ``api_key``
     - Yes
     - Statuspage API key (OAuth token).  Obtain it from **Profile → API
       info** in the Statuspage dashboard.  Stored as a password field;
       never echoed back in the UI.
   * - ``page_id``
     - Yes
     - The Statuspage page identifier.  Visible in the page URL
       (``manage.statuspage.io/pages/<page_id>``) or under **Settings →
       Page Information**.
   * - ``component_id``
     - No
     - Optional component to associate with the incident.  When set, the
       incident body includes ``component_ids`` and a ``components`` map
       whose value matches the ``initial_status``.
   * - ``initial_status``
     - No
     - Incident status on creation.  One of ``investigating`` (default),
       ``identified``, or ``monitoring``.
   * - ``name``
     - No
     - Incident title as a Go ``text/template`` rendered over the alert
       record.  Default: ``{{ .Severity }}: {{ .Host }}``.
   * - ``body``
     - No
     - Incident update body as a Go ``text/template``.  Default:
       ``{{ .Message }}``.
   * - ``impact``
     - No
     - Optional impact level override sent as ``impact_override``.  One of
       ``minor``, ``major``, or ``critical``.  Leave empty to let
       Statuspage derive the impact from the component statuses.
   * - ``api_base``
     - No
     - Base URL for the Statuspage API.  Override only for self-hosted
       Statuspage installations.  Default:
       ``https://api.statuspage.io``.
   * - ``timeout``
     - No
     - HTTP request timeout as a Go duration string (e.g. ``10s``,
       ``30s``).  Default: ``10s``.

Field reference
---------------

.. code-block:: yaml
   :caption: Example action_form values

   api_key: "YOUR-STATUSPAGE-API-KEY"
   page_id: "abc1def2ghi3"
   component_id: "jkl4mno5pqr6"
   initial_status: "investigating"
   name: "{{ .Severity }}: {{ .Host }}"
   body: "{{ .Message }}"
   impact: "major"
   api_base: "https://api.statuspage.io"
   timeout: "10s"

Template variables map directly to the ``snoozetypes.Record`` fields:
``.UID``, ``.Host``, ``.Source``, ``.Process``, ``.Severity``, ``.Message``,
``.Timestamp``, ``.Tags``, ``.State``, etc.

End-to-end test setup
=====================

To exercise the plugin against a real Statuspage account:

1. **Create a Statuspage account** at
   https://manage.statuspage.io and create at least one page.

2. **Generate an API key**: navigate to *Profile → API info* and click
   *Generate key*.  Copy the key; it is only shown once.

3. **Find your page ID**: the page ID appears in the URL after
   ``manage.statuspage.io/pages/`` when viewing the page dashboard, or under
   *Settings → Page Information*.

4. **Export the environment variables** and run the e2e test:

.. code-block:: console

   $ export SNOOZE_E2E_STATUSPAGE_API_KEY="YOUR-STATUSPAGE-API-KEY"
   $ export SNOOZE_E2E_STATUSPAGE_PAGE_ID="YOUR-PAGE-ID"
   $ go test -run E2E ./internal/pluginimpl/statuspage/...

The test creates one incident (visible briefly in the Statuspage UI) and
then immediately resolves it.  No permanent state is left on the page.

Environment variables read by the e2e test:

.. list-table::
   :header-rows: 1

   * - Variable
     - Description
   * - ``SNOOZE_E2E_STATUSPAGE_API_KEY``
     - Statuspage API key (OAuth token).
   * - ``SNOOZE_E2E_STATUSPAGE_PAGE_ID``
     - Statuspage page identifier.

Notes & limitations
===================

**Resolve by name matching (known limitation)**
  Statuspage has no structured external correlation key on incidents.  The
  plugin resolves incidents by comparing the rendered ``name`` template value
  against the ``name`` field of each unresolved incident returned by
  ``GET /v1/pages/{page_id}/incidents/unresolved``.

  This means:

  * If the incident was renamed in the Statuspage UI after creation, the
    auto-resolve will not find it and will log a no-op.
  * If multiple unresolved incidents share the same name, the plugin resolves
    the last one in the API response (which is typically the most recently
    created incident, since the API returns incidents in reverse chronological
    order).
  * To guarantee reliable correlation, keep the name template unique per
    alert origin — for example, include ``.Host`` and ``.Source`` in the
    name.

**HTTP 201 required for create**
  The Statuspage API returns HTTP 201 on a successful incident creation.  Any
  other status code is treated as an error and surfaced to the Snooze
  notification worker for retry / dead-letter handling.

**Component status on resolve**
  The plugin currently only sets the incident status to ``resolved``; it does
  not reset individual component statuses.  Use the Statuspage automation
  rules or a follow-up action to update component statuses if required.

**API rate limits**
  Statuspage imposes per-key rate limits.  Consult the
  `Statuspage API documentation <https://developer.statuspage.io/>`_ for the
  current limits.  The plugin does not implement automatic back-off or retry;
  rate-limit errors are returned to the notification worker.
