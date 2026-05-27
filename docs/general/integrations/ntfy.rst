.. _integration-ntfy:

========================
ntfy (output)
========================

Overview
========

The **ntfy** plugin is an outbound (notifier) integration that delivers mobile
push notifications via a self-hosted or public `ntfy <https://ntfy.sh>`_ server.
It is an in-process Snooze plugin — no auxiliary daemon is needed.

On each alert, the plugin renders a title and a message body from Go
``text/template`` strings evaluated over the alert record, then POSTs the body
as plain text to ``{server}/{topic}`` with ntfy-specific HTTP headers
(``Title``, ``Priority``, ``Tags``, ``Click``).  Severity is automatically
mapped to an ntfy priority (1–5) and a set of emoji tags unless the operator
overrides them.

Authentication supports either a Bearer token (ntfy access-control token) or
HTTP Basic auth (username + password).

Configuration
=============

Wire the plugin from the Snooze web UI or a YAML notification action by
choosing **"Send an ntfy notification"** and filling in the fields below.

Field reference
---------------

.. code-block:: yaml
    :caption: Example action form values

    server:   https://ntfy.example.com   # default: https://ntfy.sh
    topic:    ops-alerts                 # required
    title:    "{{ .Severity }} on {{ .Host }}"   # Go template; default shown
    message:  "{{ .Message }}"                    # Go template; default shown
    priority: auto          # auto | 1 (min) .. 5 (max); default: auto
    tags:                   # leave blank to derive from severity
    click:    https://snooze.example.com/alerts   # optional tap-to-open URL
    token:    tk_xxxx       # Bearer token (optional)
    username: alice         # Basic auth username (optional, used when token is empty)
    password: secret        # Basic auth password (optional)
    timeout:  10s           # Go duration; default 10s

**Field descriptions:**

``server``
    Base URL of the ntfy server.  Defaults to the public ``https://ntfy.sh``
    instance.  For a self-hosted server use e.g. ``https://ntfy.example.com``.

``topic``
    The ntfy topic name to publish to.  Subscribers must subscribe to the same
    topic on their ntfy client.  **Required.**

``title``
    Notification title rendered as a Go ``text/template`` over the alert record.
    Available fields: ``.UID``, ``.Host``, ``.Source``, ``.Process``,
    ``.Severity``, ``.Message``, ``.Timestamp``, ``.Tags``, etc.
    Default: ``{{ .Severity }} on {{ .Host }}``.

``message``
    Notification body, also a Go template.  Default: ``{{ .Message }}``.

``priority``
    ntfy priority integer (1 = min … 5 = max) or the special value ``auto``
    (default) which derives the priority from the alert severity:

    ============= ======== =================
    Severity      Priority Tags
    ============= ======== =================
    emergency     5        rotating_light
    critical      5        rotating_light
    error / err   4        warning
    warning       4        warning
    notice        2        information_source
    info          2        information_source
    debug         2        information_source
    *(unknown)*   2        information_source
    ============= ======== =================

``tags``
    Comma-separated list of ntfy emoji tag names (e.g. ``warning,skull``).
    When left empty, tags are derived from the severity as shown above.

``click``
    Optional URL that the ntfy app opens when the user taps the notification.

``token``
    Bearer token for ntfy `Access Control
    <https://docs.ntfy.sh/publish/#access-control>`_.  When set, the plugin
    sends ``Authorization: Bearer <token>`` and ignores ``username``/``password``.

``username`` / ``password``
    HTTP Basic auth credentials.  Used only when ``token`` is empty.

``timeout``
    Per-request HTTP timeout expressed as a Go duration string (e.g. ``10s``,
    ``500ms``).  Defaults to ``10s``.

End-to-end test setup
=====================

To exercise the plugin against a live ntfy server:

1. Choose a unique topic name (e.g. ``snooze-e2e-<yourname>``).
2. Subscribe to that topic in the ntfy mobile app or browser
   (``https://ntfy.sh/<topic>``).
3. Export the required environment variables and run the test:

.. code-block:: console

    $ export SNOOZE_E2E_NTFY_TOPIC="snooze-e2e-myname"

    # Optional: use a self-hosted server
    $ export SNOOZE_E2E_NTFY_SERVER="https://ntfy.example.com"

    # Optional: Bearer token (ntfy access control)
    $ export SNOOZE_E2E_NTFY_TOKEN="tk_xxxx"

    # Optional: Basic auth
    $ export SNOOZE_E2E_NTFY_USERNAME="alice"
    $ export SNOOZE_E2E_NTFY_PASSWORD="secret"

    $ go test -run TestNtfyE2E ./internal/pluginimpl/ntfy/...

A notification titled **"Snooze E2E test"** should appear on all subscribed
devices within a few seconds.

**Environment variables summary:**

+--------------------------------+------------------------------------------------------+-----------+
| Variable                       | Description                                          | Required  |
+================================+======================================================+===========+
| ``SNOOZE_E2E_NTFY_TOPIC``      | ntfy topic name to publish to                        | Yes       |
+--------------------------------+------------------------------------------------------+-----------+
| ``SNOOZE_E2E_NTFY_SERVER``     | ntfy server base URL (default: ``https://ntfy.sh``) | No        |
+--------------------------------+------------------------------------------------------+-----------+
| ``SNOOZE_E2E_NTFY_TOKEN``      | Bearer token for access control                      | No        |
+--------------------------------+------------------------------------------------------+-----------+
| ``SNOOZE_E2E_NTFY_USERNAME``   | Basic auth username                                  | No        |
+--------------------------------+------------------------------------------------------+-----------+
| ``SNOOZE_E2E_NTFY_PASSWORD``   | Basic auth password                                  | No        |
+--------------------------------+------------------------------------------------------+-----------+

Notes & limitations
===================

- **HTTPS only for public ntfy.sh**: the public server is HTTPS; self-hosted
  servers may use HTTP on non-standard ports — ensure your ``server`` URL uses
  the correct scheme.
- **No TLS-skip option**: unlike the ``webhook`` plugin, the ntfy plugin does
  not expose a ``tls_insecure`` knob.  If you need to skip verification for a
  self-hosted server with a self-signed certificate, route traffic through the
  ``webhook`` plugin instead.
- **No resolve/close path**: ntfy push does not have a native concept of alert
  resolution.  When an alert closes, Snooze will call ``Send`` with
  ``rec.State == "close"`` — the plugin sends it as a normal notification; the
  message template can branch on ``{{ if eq .State "close" }}resolved{{ end }}``.
- **Rate limits**: the public ``ntfy.sh`` instance limits the publish rate per
  topic/IP.  Self-hosted instances have configurable limits via
  ``visitor-request-limit-*`` in the ntfy server config.
- **ntfy gRPC / WebSocket subscribe** is not supported — this plugin only
  publishes (output direction).
