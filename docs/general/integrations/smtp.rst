.. _integration-smtp:

==============================
SMTP (input)
==============================

Overview
========

**snooze-smtp** is a standalone daemon that acts as an inbound SMTP server.
Every mail message delivered to it is parsed and converted into a Snooze alert.
It is a separate process — not an in-process plugin — that owns a long-lived
TCP listener (by default on port 25) and forwards alert records to
``snooze-server`` via ``pkg/snoozeclient``.

The daemon speaks enough of RFC 5321 to accept a single envelope per
connection: ``HELO``/``EHLO``, ``MAIL FROM``, ``RCPT TO``, ``DATA``, and
``QUIT``. Optional extensions are advertised only when configured:
``STARTTLS`` when ``tls_cert`` and ``tls_key`` are both set, and ``AUTH
PLAIN`` when ``auth_required`` is enabled.

What it ingests
---------------

Each accepted mail message becomes one ``snoozetypes.Record``:

============================  ====================================================
Snooze field                  Mail source
============================  ====================================================
``source``                    constant ``"smtp"``
``host``                      domain part of ``MAIL FROM`` (local domain stripped
                              if it matches ``local_domains``); falls back to
                              the ``from`` token of the first ``Received``
                              header
``process``                   the mail ``Subject`` header (or ``"smtp"`` when
                              empty)
``severity``                  keyword scan of the ``Subject`` (see below);
                              defaults to ``"warning"``
``message``                   plain-text body (HTML stripped for ``text/html``
                              parts; first ``text/plain`` wins in multipart)
``timestamp``                 RFC 5322 ``Date`` header, or receive time on
                              parse failure
``raw.from``                  ``MAIL FROM`` envelope address
``raw.to``                    list of ``RCPT TO`` addresses
``raw.peer``                  remote ``host:port`` from the SMTP session
``raw.helo``                  ``HELO``/``EHLO`` value from the client
``raw.subject``               decoded ``Subject`` header
``raw.headers``               all RFC 5322 headers as a ``key→value`` map
``raw.cc``                    ``Cc`` header (when present)
``raw.fqdn``                  fully-qualified ``MAIL FROM`` domain (when
                              ``local_domains`` stripping produced a shorter
                              ``host``)
``raw.auth_user``             ``AUTH PLAIN`` username (when AUTH succeeded)
============================  ====================================================

Severity mapping
----------------

The daemon scans the ``Subject`` header for whole-word matches (case-insensitive):

=================================  ================
Subject keyword                    Snooze severity
=================================  ================
``fatal``, ``critical``, ``crit``  ``crit``
``alert``                          ``alert``
``emerg``, ``emergency``           ``emerg``
``error``, ``err``                 ``err``
``warning``, ``warn``              ``warning``
``notice``                         ``notice``
``info``, ``ok``, ``success``      ``info``
``debug``                          ``debug``
=================================  ================

The first match in the order above wins. Subjects with no recognisable keyword
produce severity ``"warning"``.

Configuration
=============

``snooze-smtp`` reads ``/etc/snooze/smtp.yaml`` by default; override the path
with ``-config``.

.. code-block:: yaml
   :caption: /etc/snooze/smtp.yaml

   # --- Snooze server (where alerts are POSTed) ---
   server: "https://snooze.example.com"    # Required
   username: "ingest"
   password: "change-me"
   method: "local"             # auth backend: local | ldap | anonymous
   # token: ""                 # bearer token (skips username/password)
   insecure: false             # disable TLS verification for the Snooze client

   # --- SMTP listener ---
   listen: "0.0.0.0:25"       # TCP bind address (default: 0.0.0.0:25)
   hostname: ""                # SMTP banner hostname; defaults to os.Hostname()

   # --- TLS (optional; both must be set to enable STARTTLS) ---
   # tls_cert: /etc/snooze/smtp.crt
   # tls_key:  /etc/snooze/smtp.key

   # --- Sender filtering ---
   allowed_senders:            # glob patterns matched against MAIL FROM
     - "*"                     # (default) accept any sender
   # Examples:
   #   - "alerts@example.com"
   #   - "*@monitoring.example.com"

   # --- AUTH PLAIN (optional) ---
   auth_required: false        # require inbound clients to authenticate

   # --- Host normalisation ---
   local_domains: []           # domains to strip from MAIL FROM for Record.Host
   # Example: ["example.com"]
   # alerts@web-01.example.com → host=web-01

   # --- Tuning ---
   max_message_bytes: 10485760 # max DATA size in bytes (default: 10 MiB)
   request_timeout: 10s        # per-alert POST timeout (default: 10s)
   read_timeout: 60s           # per-client SMTP read deadline (default: 60s)
   write_timeout: 60s          # per-client SMTP write deadline (default: 60s)

Field reference
---------------

============================  ==================================================
Key                           Meaning
============================  ==================================================
``server``                    Snooze base URL. **Required.**
``username`` / ``password``   Credentials for the v1 ``/login`` endpoint.
``method``                    Auth backend; defaults to ``local``.
``token``                     Bearer token; short-circuits login when set.
``insecure``                  Skip TLS verification for the Snooze client.
``listen``                    TCP bind address; defaults to ``0.0.0.0:25``.
``hostname``                  SMTP banner / EHLO hostname; defaults to
                              ``os.Hostname()`` with ``"snooze-smtp"`` fallback.
``tls_cert`` / ``tls_key``    PEM certificate and key paths. Both must be set to
                              advertise ``STARTTLS``. Implicit TLS (port 465) is
                              not supported.
``allowed_senders``           Glob patterns matched case-insensitively against
                              the ``MAIL FROM`` address. ``"*"`` (default) accepts
                              everything. Supported wildcards: ``*@domain`` and
                              ``*@*.domain``.
``auth_required``             When ``true``, inbound clients must present
                              ``AUTH PLAIN`` credentials matching
                              ``username``/``password``. Requires ``username``
                              to be set.
``local_domains``             Domains whose suffix is stripped from
                              ``MAIL FROM`` to produce a short ``Record.Host``.
                              Supports the ``*.domain`` wildcard.
``max_message_bytes``         Maximum ``DATA`` section size; defaults to
                              ``10485760`` (10 MiB). Excess triggers ``552``.
``request_timeout``           Per-request timeout; defaults to ``10s``.
``read_timeout``              Per-client SMTP read deadline; defaults to ``60s``.
``write_timeout``             Per-client SMTP write deadline; defaults to ``60s``.
============================  ==================================================

systemd unit
------------

.. code-block:: ini
   :caption: /etc/systemd/system/snooze-smtp.service

   [Unit]
   Description=Snooze SMTP ingestion daemon
   Documentation=https://github.com/snoozeweb/snooze
   After=network-online.target snooze-server.service
   Wants=network-online.target

   [Service]
   Type=simple
   User=snooze
   Group=snooze
   ExecStart=/usr/bin/snooze-smtp -config /etc/snooze/smtp.yaml
   Restart=on-failure
   RestartSec=5s

   ProtectSystem=strict
   ProtectHome=true
   PrivateTmp=true
   NoNewPrivileges=true
   ReadWritePaths=/var/lib/snooze /var/log/snooze
   AmbientCapabilities=CAP_NET_BIND_SERVICE
   CapabilityBoundingSet=CAP_NET_BIND_SERVICE

   StandardOutput=journal
   StandardError=journal

   [Install]
   WantedBy=multi-user.target

``CAP_NET_BIND_SERVICE`` is required to bind TCP port 25. If you configure an
unprivileged port (e.g. ``listen: "0.0.0.0:2525"``), remove both
``AmbientCapabilities`` and ``CapabilityBoundingSet`` lines.

Usage
=====

Point your monitoring tool or mail relay at the snooze-smtp daemon as though it
were any SMTP server.

Postfix relay
-------------

Add a ``transport_maps`` entry so Postfix routes a specific recipient domain to
snooze-smtp:

.. code-block:: text
   :caption: /etc/postfix/transport

   alerts.example.com    smtp:[snooze-host]:25

Then ``postmap /etc/postfix/transport`` and add
``transport_maps = hash:/etc/postfix/transport`` to ``main.cf``.

Sendmail / nullmailer
---------------------

Configure the MTA to relay to ``snooze-host:25`` for alert addresses:

.. code-block:: console

   $ echo 'snooze-host' > /etc/nullmailer/remotes

Monitoring tools
----------------

Most monitoring systems (Nagios, Icinga, Zabbix, Alertmanager) support an SMTP
notification channel. Point the channel at snooze-host port 25 with no
authentication (or ``AUTH PLAIN`` if ``auth_required: true``).

Testing / verifying
===================

Send a test mail with ``swaks`` (Swiss Army Knife for SMTP):

.. code-block:: console

   $ swaks \
       --to   alerts@snooze-host \
       --from "nagios@monitoring.example.com" \
       --server snooze-host:25 \
       --header "Subject: CRITICAL - disk usage at 92% on web-01" \
       --body "Filesystem /var is at 92% capacity."

Or with ``curl``'s SMTP support:

.. code-block:: console

   $ curl -sS smtp://snooze-host:25 \
       --mail-from "nagios@monitoring.example.com" \
       --mail-rcpt "alerts@snooze-host" \
       --upload-file - <<'EOF'
   From: nagios@monitoring.example.com
   To: alerts@snooze-host
   Subject: WARNING - load average high on db-02

   Load average on db-02 is 4.2 (threshold 2.0).
   EOF

To confirm the record arrived, query the Snooze API:

.. code-block:: console

   $ curl -sS -H 'Authorization: Bearer <token>' \
       'https://snooze.example.com/api/v1/record' \
       | jq '.[] | select(.source=="smtp") | {host,process,severity,message}'

Notes & limitations
===================

- **STARTTLS only, no implicit TLS.** Implicit TLS (SMTPS / port 465) is not
  supported. Set ``tls_cert`` and ``tls_key`` to advertise ``STARTTLS`` on port
  25 or 587.
- **Single-part plain-text recommended.** Body extraction handles
  ``text/plain``, ``text/html`` (tags stripped), and
  ``multipart/alternative``/``mixed``/``related`` recursively. The first
  ``text/plain`` part wins; it falls back to stripped HTML. Complex MIME trees
  (e.g. calendar invites) may produce empty bodies — the alert is still created
  using envelope and subject data.
- **Charset transcoding is not performed.** Bodies are forwarded as received
  bytes. For ISO-8859-1 or other legacy encodings, configure your MTA to
  transcode to UTF-8 before relaying.
- **Sender filtering is post-RCPT.** The ``allowed_senders`` check runs after
  the ``DATA`` command is accepted. Rejected senders receive a ``550`` response
  and the message is discarded without being forwarded.
- **No queuing.** A ``PostAlert`` failure is logged but does not cause the
  daemon to reject the mail — the sender already received ``250 OK``. Use Snooze
  aggregate rules and Snooze's own persistence for at-least-once guarantees
  downstream.
- **No multi-recipient fanout.** Each accepted message produces exactly one
  alert record regardless of how many ``RCPT TO`` addresses were given.
