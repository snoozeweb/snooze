.. _integration-rest-api:

======================
Snooze REST API (input)
======================

Overview
========

The Snooze server exposes a generic alert-ingest endpoint at
``POST /api/v1/alerts``. Any monitoring tool, script, or custom
integration that can speak JSON over HTTP can send alerts directly to
Snooze without installing a plugin. This page covers:

- the HTTP endpoint and its wire contract,
- the ``snooze`` CLI for use from scripts and cron jobs,
- the ``snooze-client`` Python library for use from Python code.

The endpoint is **public by default** (no Bearer JWT required). This
preserves parity with the Python 1.x ``AlertRoute`` ingest path, which
was likewise unauthenticated. If you need to restrict ingest, place
Snooze behind a reverse proxy that filters by source IP or adds an
authentication layer.

HTTP endpoint
=============

Endpoint
--------

.. code-block:: text

   POST /api/v1/alerts
   Content-Type: application/json

The request body is either a single JSON object or a JSON array of
objects. Every object is run through the configured
``core.process_plugins`` pipeline (rules, aggregation, notifications,
…) before the response is returned.

Field reference
---------------

All fields are optional. Unknown fields are preserved in the record and
forwarded through the pipeline unchanged, so you can include any
additional key-value pairs you need (e.g. ``env``, ``team``,
``ticket_id``).

.. list-table::
   :widths: 18 15 67
   :header-rows: 1

   * - Field
     - Type
     - Description
   * - ``host``
     - string
     - Name of the host issuing the alert (e.g. ``web-1``).
   * - ``source``
     - string
     - Source system or monitoring tool (e.g. ``nagios``, ``custom``).
   * - ``process``
     - string
     - Process or service name within the host.
   * - ``severity``
     - string
     - Alert severity. Any string is accepted; the recommended vocabulary
       follows `syslog severity keywords
       <https://en.wikipedia.org/wiki/Syslog#Severity_level>`_:
       ``emerg``, ``alert``, ``crit``, ``err``, ``warning``, ``notice``,
       ``info``, ``debug``.
   * - ``message``
     - string
     - Human-readable alert description.
   * - ``timestamp``
     - string (ISO 8601)
     - Alert timestamp. Any format parseable by dateutil is accepted
       (e.g. ``2024-01-15T10:30:00+00:00``). Defaults to the server's
       receive time when absent.
   * - ``environment``
     - string
     - Logical environment (e.g. ``prod``, ``staging``).
   * - ``tags``
     - array of strings
     - Arbitrary tags for routing and filtering.
   * - ``state``
     - string (enum)
     - Initial record state. One of ``open``, ``ack``, ``close``,
       ``shelved``. Defaults to ``open``. Set to ``close`` to emit a
       resolution event.
   * - ``ttl``
     - integer (seconds)
     - Time-to-live hint: the record expires (auto-closed) after this
       many seconds.
   * - ``raw``
     - object
     - Arbitrary freeform payload stored verbatim alongside the record.
       Useful for carrying source-specific data that does not map to the
       standard schema.
   * - *(any extra key)*
     - any
     - Extra fields are preserved in the record and made available to
       rules and templates.

Response
--------

``HTTP 200`` is returned for every request, even when individual records
fail pipeline processing. The response body carries two keys:

.. code-block:: json

   {
     "data":   [ ... ],
     "errors": [ "some processing error", "..." ]
   }

``data`` is the array of post-pipeline records (may be empty when all
records errored); ``errors`` is an array of per-record error strings
and is omitted from the response when all records succeeded.

curl example
------------

Submit a single alert:

.. code-block:: console

   $ curl -s -X POST https://snooze.example.com/api/v1/alerts \
       -H 'Content-Type: application/json' \
       -d '{
         "host":      "web-1",
         "source":    "custom-monitor",
         "severity":  "err",
         "message":   "Disk usage exceeded 90% on /var",
         "timestamp": "2024-01-15T10:30:00+00:00",
         "tags":      ["team:ops", "env:prod"]
       }'

Expected response::

   {"data":[{"host":"web-1","source":"custom-monitor","severity":"err",...}]}

Submit multiple alerts in one call:

.. code-block:: console

   $ curl -s -X POST https://snooze.example.com/api/v1/alerts \
       -H 'Content-Type: application/json' \
       -d '[
         {"host": "db-1", "severity": "crit", "message": "Connection pool exhausted"},
         {"host": "db-2", "severity": "warning", "message": "Replication lag 120s"}
       ]'

Send a resolution (close) event:

.. code-block:: console

   $ curl -s -X POST https://snooze.example.com/api/v1/alerts \
       -H 'Content-Type: application/json' \
       -d '{"host": "web-1", "source": "custom-monitor", "state": "close",
            "message": "Disk usage back to normal"}'

CLI
===

The ``snooze`` CLI is a thin wrapper around the HTTP API, useful in
shell scripts, cron jobs, and one-off tests.

Installation
------------

On the Snooze server:

.. code-block:: console

   $ sudo /opt/snooze/bin/pip install snooze-client

On any other node:

.. code-block:: console

   $ sudo pip3 install snooze-client

Server address
--------------

All CLI commands read the server URL from ``/etc/snooze/client.yaml``:

.. code-block:: yaml
   :caption: /etc/snooze/client.yaml

   server: https://snooze.example.com:5200

Usage
-----

Fields are supplied as ``key=value`` pairs. Spaces in values are handled
by standard shell quoting; ``=`` is not allowed in field names but is
allowed in values.

.. code-block:: console
   :caption: Basic alert

   $ snooze alert host=myhost01 severity=err \
       "message=Disk usage exceeded 90% on /var"

.. code-block:: console
   :caption: Alert with explicit timestamp and custom fields

   $ snooze alert "timestamp=$(date -Is)" host=myhost01 severity=err \
       custom_field=custom_system "message=Alert on custom system"

The above produces the following JSON record::

   {
     "timestamp": "2021-07-01T22:30:00+09:00",
     "host":      "myhost01",
     "custom_field": "custom_system",
     "message":   "Alert on custom system"
   }

No fields are mandatory.

Python client library
=====================

For Python scripts and integration code, ``snooze-client`` exposes a
``Snooze`` object with an ``alert`` method. All values in the dictionary
must be JSON-serialisable (``str``, ``int``, ``float``, ``dict``,
``list``).

.. code-block:: python
   :caption: Example

   from snooze_client import Snooze
   from datetime import datetime

   # Reads server address from /etc/snooze/client.yaml
   api = Snooze()

   timestamp = datetime.now().astimezone().isoformat()
   alert = {
       'host':      'myhost01',
       'message':   'my alert',
       'severity':  'err',
       'timestamp': timestamp,
   }
   api.alert(alert)

See the `snooze-client documentation
<https://github.com/snoozeweb/snooze_client>`_ for the full API surface,
including bulk sends and result handling.

Testing / verifying
===================

The quickest way to verify that alert ingest is working is to send a
test record with curl and inspect the Snooze web UI or query the records
API:

.. code-block:: console

   # Send the test alert
   $ curl -s -X POST https://snooze.example.com/api/v1/alerts \
       -H 'Content-Type: application/json' \
       -d '{"host":"test-host","severity":"info","message":"connectivity check"}'

   # Confirm it landed (requires a Bearer JWT)
   $ TOKEN=$(curl -s -X POST https://snooze.example.com/api/v1/login/local \
       -H 'Content-Type: application/json' \
       -d '{"username":"admin","password":"..."}' | jq -r .token)
   $ curl -s https://snooze.example.com/api/v1/record \
       -H "Authorization: Bearer $TOKEN" | jq '.data[-1]'

Notes & limitations
===================

- **Unauthenticated by default.** The ``POST /api/v1/alerts`` endpoint
  carries no ``security`` requirement in the OpenAPI contract (matching
  Python 1.x ``AlertRoute`` behaviour). All other endpoints outside the
  login family require a Bearer JWT; see ``api/openapi.yaml`` for the
  full security model.
- **Batch ingest.** Sending an array rather than a single object is
  supported natively. Each element is processed independently; a
  pipeline error on one record does not abort processing of the others.
- **No required fields.** Every field is optional. Records with only a
  ``message`` field (or even no fields at all) are accepted and flow
  through the pipeline normally.
- **Extra fields are preserved.** Any JSON key not listed in the schema
  is stored in the record and is available to rules, templates, and
  downstream plugins.
- **Idempotency.** The endpoint is not idempotent. Submitting the same
  payload twice creates two separate records (subject to aggregate-rule
  deduplication configured in the pipeline).
- **Content-Type.** The request must carry ``Content-Type:
  application/json``. Requests with a missing or non-JSON content type
  will be rejected.
