.. _integration-discord:

=======================
Discord (output)
=======================

Overview
========

The **discord** plugin is an in-process Notifier that posts alert
notifications to a Discord channel via a Discord `Incoming Webhook
<https://discord.com/developers/docs/resources/webhook>`_.

When a notification rule matches a record, the plugin renders a message
from the configured template and POSTs it to the webhook URL as JSON.
It supports two presentation modes:

- **Embed mode** (default): sends a rich embed with a colour-coded severity
  indicator, a title derived from the alert host and source, a description
  rendered from the message template, per-alert fields (Source, Severity),
  and a timestamp.
- **Plain mode**: sends a flat ``content`` text message rendered from the
  same template.

The plugin uses ``net/http`` only — no Discord SDK or other external
library is required.

Configuration
=============

Wire the plugin through a **Notification → Action** in the Snooze UI or
configuration file. Set the action type to ``discord`` and fill the
``action_form`` fields described below.

Field reference
---------------

.. list-table::
   :widths: 20 10 70
   :header-rows: 1

   * - Field
     - Default
     - Description
   * - ``webhook_url``
     - *(required)*
     - Discord Incoming Webhook URL (``https://discord.com/api/webhooks/<id>/<token>``).
   * - ``username``
     - *(bot name)*
     - Override the display name of the webhook bot.
   * - ``avatar_url``
     - *(bot avatar)*
     - URL of an image to use as the bot avatar.
   * - ``message``
     - ``**{{ .Severity }}** on {{ .Host }}: {{ .Message }}``
     - Go ``text/template`` rendered over the alert record. Available
       fields: ``.UID``, ``.Host``, ``.Source``, ``.Process``,
       ``.Severity``, ``.Message``, ``.State``, ``.Timestamp``,
       ``.Tags``.
   * - ``use_embed``
     - ``true``
     - When ``true`` (default) send a rich Discord embed. When ``false``
       send a plain ``content`` text message.
   * - ``timeout``
     - ``10s``
     - HTTP request timeout as a Go duration string (e.g. ``5s``, ``30s``).

.. code-block:: yaml
   :caption: Example action_form values

   webhook_url: "https://discord.com/api/webhooks/1234567890/abcdefghijklmnop"
   username: "Snooze Alerts"
   avatar_url: "https://example.com/snooze-logo.png"
   message: "**{{ .Severity }}** on {{ .Host }}: {{ .Message }}"
   use_embed: true
   timeout: "10s"

Severity colour mapping
-----------------------

The embed colour is derived from the record's ``severity`` field:

.. list-table::
   :widths: 30 20 50
   :header-rows: 1

   * - Severity
     - Colour
     - Hex
   * - ``info``, ``notice``, ``debug``, *(unknown)*
     - Green
     - ``#36a64f``
   * - ``warning``
     - Amber
     - ``#daa038``
   * - ``error``, ``err``, ``critical``, ``emergency``
     - Red
     - ``#d00000``
   * - ``close`` *(resolved)*
     - Teal
     - ``#2eb886``

When the record's ``state`` field is ``"close"`` (a resolution event),
the teal resolved colour is used regardless of the ``severity`` field.

End-to-end test setup
=====================

To run the end-to-end test you need a Discord channel with an Incoming
Webhook configured.

1. Open your Discord server settings → **Integrations** → **Webhooks**.
2. Click **New Webhook**, choose a channel, and copy the webhook URL.
3. Export the URL and run the test:

.. code-block:: console

   $ export SNOOZE_E2E_DISCORD_WEBHOOK="https://discord.com/api/webhooks/<id>/<token>"
   $ go test -run TestDiscordE2E ./internal/pluginimpl/discord/...

The test sends one embed message to the channel and asserts no error is
returned. Inspect the channel to verify the message appearance.

**Environment variables read by the e2e test:**

.. list-table::
   :widths: 40 60
   :header-rows: 1

   * - Variable
     - Purpose
   * - ``SNOOZE_E2E_DISCORD_WEBHOOK``
     - Full Discord Incoming Webhook URL. The test is skipped when this
       variable is unset.

Notes & limitations
===================

- Only Incoming Webhooks are supported. Bot-token based delivery
  (``POST /channels/{channel.id}/messages``) is not implemented.
- Discord rate-limits webhooks to 30 requests per minute per webhook
  URL. The plugin does not implement client-side rate limiting or
  automatic retries; the caller (notification worker) is responsible for
  retry and dead-letter handling.
- The ``timeout`` field controls the full HTTP round-trip. Discord's
  webhook endpoint is generally responsive; the default ``10s`` is
  sufficient for most deployments.
- File attachments are not supported.
