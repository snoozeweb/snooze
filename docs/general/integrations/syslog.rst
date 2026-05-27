.. _integration-syslog:

==============================
Syslog (input)
==============================

Overview
========

**snooze-syslog** is a standalone daemon that ingests syslog messages over UDP
and/or TCP (RFC 3164 and RFC 5424) and forwards each parsed message to
``snooze-server`` as an alert. It is a separate process — not an in-process
plugin — so it can own long-lived network listeners and forward records to
``snooze-server`` via ``pkg/snoozeclient``.

The parser uses `leodido/go-syslog v4 <https://github.com/leodido/go-syslog>`_
with best-effort mode enabled, meaning partially-malformed messages still
produce a useful alert rather than being dropped silently.

What it ingests
---------------

Each syslog line becomes one ``snoozetypes.Record`` with the following field
mapping:

============================  ====================================================
Snooze field                  Syslog source
============================  ====================================================
``source``                    constant ``"syslog"``
``host``                      syslog ``HOSTNAME`` field
``process``                   ``APPNAME`` (TAG field for RFC 3164), or ``PROCID``
                              if APPNAME is empty
``severity``                  syslog severity number mapped to name (see below)
``message``                   syslog message body
``timestamp``                 syslog timestamp, or receive time when absent
``raw.format``                ``"rfc3164"`` or ``"rfc5424"``
``raw.facility``              syslog facility name (``kern``, ``user``, …)
``raw.original``              the raw line as received
``raw.peer``                  remote IP:port from the listener
``raw.msgid``                 RFC 5424 MSGID (when present)
``raw.procid``                RFC 5424 PROCID (when present and distinct from APPNAME)
``raw.structured_data``       RFC 5424 SD-ELEMENTs (when present, as nested map)
============================  ====================================================

Severity mapping
----------------

The syslog numerical severity (RFC 5424 §6.2.1) is translated to a lowercase
name:

=======  =========
Number   Name
=======  =========
0        ``emerg``
1        ``alert``
2        ``crit``
3        ``err``
4        ``warning``
5        ``notice``
6        ``info``
7        ``debug``
=======  =========

Out-of-range severity values default to ``info``.

Configuration
=============

``snooze-syslog`` reads ``/etc/snooze/syslog.yaml`` by default; override the
path with ``-config``. At least one of ``listen_udp`` or ``listen_tcp`` must be
set.

.. code-block:: yaml
   :caption: /etc/snooze/syslog.yaml

   # --- Snooze server (where alerts are POSTed) ---
   server: "https://snooze.example.com"    # Required
   username: "ingest"
   password: "change-me"
   method: "local"           # auth backend: local | ldap | anonymous
   # token: ""               # bearer token (skips username/password)
   insecure: false           # disable TLS verification for the Snooze client

   # --- Listeners ---
   listen_udp: "0.0.0.0:514"    # UDP listener; omit to disable
   listen_tcp: "0.0.0.0:6514"   # TCP listener; omit to disable

   # --- Parser ---
   parser: "auto"            # auto | rfc3164 | rfc5424 (default: auto)

   # --- Tuning ---
   request_timeout: 10s      # per-alert POST timeout (default: 10s)

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
``listen_udp``                UDP bind address. Omit to disable the UDP listener.
``listen_tcp``                TCP bind address. Omit to disable the TCP listener.
``parser``                    Message format: ``auto`` (default), ``rfc3164``,
                              or ``rfc5424``. ``auto`` inspects the PRI block.
``request_timeout``           Per-request timeout; defaults to ``10s``.
============================  ==================================================

systemd unit
------------

.. code-block:: ini
   :caption: /etc/systemd/system/snooze-syslog.service

   [Unit]
   Description=Snooze syslog ingestion daemon
   Documentation=https://github.com/snoozeweb/snooze
   After=network-online.target snooze-server.service
   Wants=network-online.target

   [Service]
   Type=simple
   User=snooze
   Group=snooze
   ExecStart=/usr/bin/snooze-syslog -config /etc/snooze/syslog.yaml
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

``CAP_NET_BIND_SERVICE`` is granted so the daemon can bind the privileged port
514 (standard syslog) without running as root.

Usage
=====

Point syslog sources at ``snooze-syslog`` using the standard mechanisms for
your log shipper.

rsyslog
-------

Forward all messages over TCP (RFC 5424, octet-counted framing):

.. code-block:: text
   :caption: /etc/rsyslog.d/99-snooze.conf

   # RFC 5424 over TCP
   *.* @@snooze-host:6514;RSYSLOG_SyslogProtocol23Format

   # Or plain RFC 3164 over UDP
   # *.* @snooze-host:514

After editing, restart rsyslog:

.. code-block:: console

   $ sudo systemctl restart rsyslog

syslog-ng
---------

.. code-block:: text

   destination d_snooze { tcp("snooze-host" port(6514)); };
   log { source(s_src); destination(d_snooze); };

systemd-journal-remote / journald
-----------------------------------

Set ``ForwardToSyslog=yes`` in ``/etc/systemd/journald.conf`` and let the local
syslog daemon forward to snooze-syslog as above.

Testing / verifying
===================

Send a test message with ``logger`` (RFC 3164):

.. code-block:: console

   $ logger -n snooze-host -P 514 --udp "test alert from $(hostname)"

Or inject an RFC 5424 frame over TCP with netcat:

.. code-block:: console

   $ printf '<165>1 2024-01-15T10:00:00Z web-01 myapp 12345 - - disk usage at 92%%\n' \
       | nc snooze-host 6514

To confirm the record arrived, query the Snooze API:

.. code-block:: console

   $ curl -sS -H 'Authorization: Bearer <token>' \
       'https://snooze.example.com/api/v1/record' | jq '.[] | select(.source=="syslog") | {host,message,severity}'

Notes & limitations
===================

- **Auto-detection** inspects the two bytes after the closing ``>`` of the PRI
  block. A RFC 5424 VERSION digit followed by a space selects the 5424 parser;
  everything else falls back to RFC 3164. Force a parser with ``parser:
  rfc3164`` or ``parser: rfc5424`` when sources are known to be homogeneous.
- **No octet-framed TCP by default.** The TCP listener reads newline-delimited
  frames (the traditional syslog-over-TCP convention). Octet-counted framing
  (RFC 5425) is not supported in this version; configure rsyslog to use plain
  newline framing when targeting port 6514.
- **UDP is connectionless and lossy.** Under heavy load the kernel may drop UDP
  datagrams before they reach userspace. Use TCP (or RELP — see
  :ref:`integration-relp`) for reliable delivery.
- **Login is lazy.** The daemon performs an eager login at startup and retries
  transparently on 401. A momentarily unreachable Snooze server delays the
  first alert POST but does not prevent the daemon from accepting syslog
  traffic.
- **No duplicate-suppression.** Snooze aggregate rules are the appropriate
  place to coalesce repeated messages; snooze-syslog forwards every received
  line.
