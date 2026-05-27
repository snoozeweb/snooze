.. _integration-mcp:

==========================================
Model Context Protocol server (MCP) (tool)
==========================================

Overview
========

``snooze-mcp`` is a `Model Context Protocol <https://modelcontextprotocol.io>`_
(MCP) server that exposes Snooze alerts and record actions as callable
**tools** to AI assistants such as Claude Desktop, Claude Code and Cursor. It
lets an operator triage alerts conversationally — "show me the last 5 critical
alerts", "acknowledge alert ``abc123`` with note 'investigating'", "snooze it
for 6 hours" — and have the assistant drive the Snooze v1 REST API on their
behalf.

It is a **standalone daemon** but, unlike the other Snooze bridges, it is not a
long-running network service. It speaks JSON-RPC 2.0 over **stdio**
(newline-delimited messages on stdin/stdout) and is **spawned on demand by the
AI client**. Stdout is the protocol channel, so the daemon logs only to stderr.
It implements the MCP specification by hand (``net/http`` + ``encoding/json``);
there is no MCP SDK dependency.

The server is a thin, read-and-act layer over the same REST endpoints the
Google Chat and Microsoft Teams bridges use:

- ``list_alerts`` / ``get_alert`` → ``POST /api/v1/record/search``
- ``ack_alert`` / ``close_alert`` / ``comment_alert`` → ``POST /api/v1/comment``
  (typed comment; the server's AfterCreate hook applies the state transition)
- ``snooze_alert`` → ``POST /api/v1/snooze`` (plus a best-effort ack)

Every action is stamped with ``method: "mcp"`` so it is distinguishable in the
record's audit trail.

Tool catalog
============

.. list-table::
    :header-rows: 1
    :widths: 20 50 30

    * - Tool
      - Description
      - Arguments
    * - ``list_alerts``
      - List/search alert records, most recent first.
      - ``query`` (text, optional), ``condition`` (list-form Snooze condition,
        optional), ``limit`` (int, default 20)
    * - ``get_alert``
      - Fetch a single record by UID.
      - ``uid`` (required)
    * - ``ack_alert``
      - Acknowledge an alert (records an ``ack`` comment).
      - ``uid`` (required), ``message`` (optional)
    * - ``close_alert``
      - Close/resolve an alert (records a ``close`` comment).
      - ``uid`` (required), ``message`` (optional)
    * - ``comment_alert``
      - Add a free-text comment without changing state.
      - ``uid`` (required), ``message`` (required)
    * - ``snooze_alert``
      - Snooze an alert for a window (and acknowledge it).
      - ``uid`` (required), ``duration`` (Go duration like ``6h``; omit for
        forever)

Tool results are returned as MCP ``content`` with a single ``text`` item. Read
tools (``list_alerts``, ``get_alert``) return JSON; action tools return a short
human-readable confirmation. A Snooze-side failure (e.g. unknown UID) comes back
as a normal tool result with ``isError: true`` rather than a JSON-RPC error, per
the MCP convention.

Configuration
=============

``snooze-mcp`` reads the standard Snooze-client block from
``/etc/snooze/mcp.yaml`` **and/or the environment**. The environment wins, which
is the natural fit for the stdio model: the AI client launches the binary with
the credentials in its own ``env`` block. A missing config file is tolerated as
long as the required fields are supplied via the environment.

Required: ``server`` plus authentication — either ``token`` or
``username``/``password`` (or ``method: anonymous``).

Wiring it into an MCP client
----------------------------

Most clients use a JSON config that names a ``command`` and an ``env`` block.
For **Claude Desktop** (``claude_desktop_config.json``) or **Cursor**:

.. code-block:: json
    :caption: Claude Desktop / Cursor MCP server entry

    {
      "mcpServers": {
        "snooze": {
          "command": "snooze-mcp",
          "env": {
            "SNOOZE_SERVER": "https://snooze.example.com",
            "SNOOZE_TOKEN": "eyJhbGciOi..."
          }
        }
      }
    }

Username/password instead of a token:

.. code-block:: json
    :caption: Username/password auth

    {
      "mcpServers": {
        "snooze": {
          "command": "snooze-mcp",
          "env": {
            "SNOOZE_SERVER": "https://snooze.example.com",
            "SNOOZE_USERNAME": "ai-bot",
            "SNOOZE_PASSWORD": "...",
            "SNOOZE_METHOD": "local"
          }
        }
      }
    }

Environment variables (all override the file):

.. list-table::
    :header-rows: 1
    :widths: 30 70

    * - Variable
      - Meaning
    * - ``SNOOZE_SERVER``
      - Snooze base URL (required)
    * - ``SNOOZE_TOKEN``
      - Pre-minted bearer token (skips ``/login``)
    * - ``SNOOZE_USERNAME`` / ``SNOOZE_PASSWORD``
      - Credentials for the v1 ``/login`` endpoint
    * - ``SNOOZE_METHOD``
      - Auth backend (``local``, ``ldap``, ``anonymous``); default ``local``
    * - ``SNOOZE_INSECURE``
      - ``true`` to skip TLS verification (self-signed dev only)
    * - ``SNOOZE_REQUEST_TIMEOUT``
      - Per-request timeout as a Go duration; default ``30s``
    * - ``SNOOZE_DEBUG``
      - ``true`` for debug logging (to stderr)

Config file
-----------

.. code-block:: yaml
    :caption: /etc/snooze/mcp.yaml

    server: https://snooze.example.com
    # Either a token...
    token: eyJhbGciOi...
    # ...or username/password:
    # username: ai-bot
    # password: secret
    # method: local
    insecure: false
    request_timeout: 30s
    debug: false

Verifying from the shell
------------------------

Because the protocol is line-oriented JSON you can drive it by hand:

.. code-block:: console

    $ printf '%s\n%s\n' \
        '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}' \
        '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
      | SNOOZE_SERVER=https://snooze.example.com SNOOZE_TOKEN=... snooze-mcp 2>/dev/null
    {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18",...}}
    {"jsonrpc":"2.0","id":2,"result":{"tools":[...]}}

End-to-end test setup
=====================

The package ships an env-gated end-to-end test (``TestMCPE2E``) that drives the
real JSON-RPC server against a live snooze-server: ``initialize`` →
``tools/list`` → ``list_alerts``, asserting no protocol error. It is **skipped**
unless ``SNOOZE_E2E_MCP_SERVER`` is set, so ``go test ./...`` stays green in CI.
It is read-only — it never mutates a record.

Environment variables the test reads:

.. list-table::
    :header-rows: 1
    :widths: 35 65

    * - Variable
      - Meaning
    * - ``SNOOZE_E2E_MCP_SERVER``
      - Snooze base URL (required; enables the test)
    * - ``SNOOZE_E2E_MCP_TOKEN``
      - Pre-minted bearer token (use this *or* the username/password pair)
    * - ``SNOOZE_E2E_MCP_USERNAME`` / ``SNOOZE_E2E_MCP_PASSWORD``
      - Credentials; the test logs in before driving the server
    * - ``SNOOZE_E2E_MCP_METHOD``
      - Auth backend; default ``local``
    * - ``SNOOZE_E2E_MCP_INSECURE``
      - ``true`` to skip TLS verification

.. code-block:: console

    $ export SNOOZE_E2E_MCP_SERVER="https://snooze.example.com"
    $ export SNOOZE_E2E_MCP_TOKEN="eyJhbGciOi..."   # or USERNAME/PASSWORD
    $ go test -run E2E ./internal/components/mcp/...

Notes & limitations
===================

- **Transport is stdio only.** This is a child process launched by the AI
  client, not a service. There is therefore **no systemd unit** — see the
  packaging note in the daemon's source. A long-running HTTP/SSE
  ("Streamable HTTP") MCP transport that could run as a managed service is
  possible future work; it would add a listener and a systemd unit at that
  point.
- **Protocol version.** The server implements MCP revision ``2025-06-18`` and
  negotiates per the spec: it echoes back the client's requested
  ``protocolVersion`` when one is sent (maximising interop with clients pinned
  to an older revision such as ``2024-11-05``) and otherwise advertises
  ``2025-06-18``.
- **Capabilities.** Only ``tools`` is advertised. Resources, prompts, sampling
  and elicitation are not implemented.
- **Attribution.** Comments/snoozes are stamped ``method: "mcp"`` and named
  "AI assistant via MCP" (overridable with an ``actor`` argument on a tool
  call). There is a human in the loop by design — MCP clients prompt before
  invoking a tool — but operators should still treat the configured Snooze
  credentials as granting full ack/close/comment/snooze rights.
- **Search semantics.** ``list_alerts`` ``query`` maps to the Snooze ``SEARCH``
  operator (full-text across the record); ``condition`` is passed through in
  list form (e.g. ``["=", "host", "web-1"]``). When both are supplied they are
  AND-ed.
