.. _integration-pushover:

==============================
Pushover (output)
==============================

Overview
========

The **Pushover** integration is an in-process Notifier output plugin. It
delivers mobile push notifications to Pushover devices via the
`Pushover Messages API <https://pushover.net/api>`_. Each matching alert
triggers a POST to ``https://api.pushover.net/1/messages.json`` encoded as
``application/x-www-form-urlencoded``; no external daemon is required.

The plugin renders the notification title and message as Go ``text/template``
expressions evaluated against the alert record, so operators can embed any
record field (host, severity, message, source, UID, …) in the push text.

Delivery priority can be set explicitly (``-2`` through ``2``) or derived
automatically from the record's Snooze severity level.

Configuration
=============

Wire a Pushover action to a notification rule in the Snooze UI under
*Notifications → Actions → Add Action → Send a Pushover notification*.

Field reference
---------------

.. list-table::
   :widths: 20 10 10 60
   :header-rows: 1

   * - Field
     - Required
     - Default
     - Description
   * - ``token``
     - yes
     - —
     - Pushover **application API token** (obtained when you register an app at
       https://pushover.net/apps).  Stored as a Password field.
   * - ``user``
     - yes
     - —
     - Pushover **user key** or **group key** shown at the top of
       https://pushover.net after login.
   * - ``title``
     - no
     - ``{{ .Severity }} on {{ .Host }}``
     - Notification title. Go ``text/template`` over the alert record fields:
       ``.UID``, ``.Host``, ``.Source``, ``.Process``, ``.Severity``,
       ``.Message``, ``.State``, ``.Timestamp``, ``.Tags``.
   * - ``message``
     - no
     - ``{{ .Message }}``
     - Notification body.  Same template context as ``title``.
   * - ``priority``
     - no
     - ``auto``
     - Delivery priority.  ``auto`` maps the record severity:

       - ``emergency`` / ``critical`` → 2 (emergency — adds ``retry=60``
         and ``expire=3600`` automatically)
       - ``error`` / ``err`` → 1 (high)
       - ``warning`` / ``warn`` → 0 (normal)
       - anything else (``info``, ``notice``, ``debug``) → −1 (low)

       Explicit values: ``-2`` (lowest), ``-1`` (low), ``0`` (normal),
       ``1`` (high), ``2`` (emergency).
   * - ``sound``
     - no
     - *(user default)*
     - Pushover sound name, e.g. ``alien``, ``bike``, ``classical``,
       ``none``.  Leave empty to use the user's default device sound.
   * - ``url``
     - no
     - —
     - A supplementary URL attached to the notification (e.g. a Grafana
       dashboard link).
   * - ``url_title``
     - no
     - —
     - Display title for ``url``.  Ignored when ``url`` is empty.
   * - ``api_base``
     - no
     - ``https://api.pushover.net``
     - Override the Pushover API base URL.  Used for testing; leave at the
       default in production.
   * - ``timeout``
     - no
     - ``10s``
     - Per-request HTTP timeout as a Go duration string (e.g. ``5s``,
       ``30s``).

.. code-block:: yaml
   :caption: Example action_form values

   token: "aaaBBBcccDDD111222333eeeFFF444"     # your app token
   user: "uUUvVVwWWxXX1122334455aabbccdd"       # your user/group key
   title: "{{ .Severity }} on {{ .Host }}"
   message: "{{ .Message }}"
   priority: auto
   sound: ""
   url: ""
   url_title: ""
   api_base: "https://api.pushover.net"
   timeout: "10s"

End-to-end test setup
=====================

To run the live integration test you need:

1. A **Pushover account** at https://pushover.net and at least one
   registered device.
2. A **Pushover application token** — register a new app at
   https://pushover.net/apps/build and copy the *API Token/Key*.
3. Your **Pushover user key** — shown on your dashboard at
   https://pushover.net immediately after login.

Export the two variables and run the E2E test:

.. code-block:: console

   $ export SNOOZE_E2E_PUSHOVER_TOKEN="<your-app-token>"
   $ export SNOOZE_E2E_PUSHOVER_USER="<your-user-or-group-key>"
   $ go test -run E2E ./internal/pluginimpl/pushover/...

When either variable is unset the test is skipped automatically.

Environment variables
---------------------

.. list-table::
   :widths: 40 60
   :header-rows: 1

   * - Variable
     - Description
   * - ``SNOOZE_E2E_PUSHOVER_TOKEN``
     - Pushover application API token (see *App token* above).
   * - ``SNOOZE_E2E_PUSHOVER_USER``
     - Pushover user key or group key.

Notes & limitations
===================

- **Priority 2 (emergency)** requires ``retry`` and ``expire`` parameters.
  The plugin automatically sends ``retry=60`` (retry every 60 s) and
  ``expire=3600`` (give up after 1 h) when the resolved priority is 2.
  These values are not currently configurable via the action_form; open an
  issue if you need to override them.
- **Group keys** are supported by the Pushover API as the ``user`` field;
  they work transparently with this plugin.
- **HTML in messages**: the Pushover API supports limited HTML formatting
  (``<b>``, ``<i>``, ``<a>``).  You can include these tags in your
  ``message`` template; the plugin sends the body verbatim.
- **Rate limits**: Pushover enforces a per-application monthly message
  quota (7500 messages/month on the free tier).  See
  https://pushover.net/api#limits for current limits.
- **No resolve/close path**: Pushover notifications are fire-and-forget; the
  plugin does not take special action when ``rec.State == "close"``.
