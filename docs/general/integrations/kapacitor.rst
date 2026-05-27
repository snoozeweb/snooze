.. _integration-kapacitor:

=======================
Kapacitor (input)
=======================

Overview
========

The **kapacitor** plugin is an in-process WebhookReceiver that accepts
`Kapacitor HTTP alert handler
<https://docs.influxdata.com/kapacitor/v1/reference/event_handlers/http/>`_
payloads and converts them into Snooze records. It is registered at
``/api/v1/webhook/kapacitor``.

Kapacitor's ``http()`` alert handler POSTs a JSON envelope containing an alert
``id``, ``message``, ``level``, ``time``, and an InfluxDB-style
``data.series[]`` block.  The plugin fans out across the ``data.series`` array
producing one Snooze record per series, which means a Kapacitor alert that
matches multiple hosts in a single query produces a separate record for each
one.

Configuration
=============

Inbound URL
-----------

The plugin mounts at::

    /api/v1/webhook/kapacitor

No authentication is required on this endpoint by default. See
:ref:`integrations` and :doc:`../../configuration/ingest` for hardening
options including a shared ingest token (``config.ingest.token``).

Configuring the Kapacitor http() handler
-----------------------------------------

In your TICK script, add an ``http()`` alert handler pointing to the Snooze
endpoint:

.. code-block:: text
   :caption: Example TICKscript alert handler

   stream
     |from()
       .measurement('cpu')
     |alert()
       .crit(lambda: "usage_idle" < 10)
       .warn(lambda: "usage_idle" < 20)
       .id('cpu_alert/{{ index .Tags "host" }}')
       .message('CPU critical on {{ index .Tags "host" }}: {{ .Level }}')
       .http('https://<snooze-host>/api/v1/webhook/kapacitor')

The handler sends a POST for every state transition (CRITICAL, WARNING, INFO,
OK) as well as periodic keepalive messages. Keepalive messages with an empty
``data.series`` are acknowledged by Snooze with a ``{"received":0}`` response
and do not create records.

To expose Snooze-specific metadata, add ``severity``, ``process``, or
``host`` tags to the series in InfluxDB; the plugin pops these from
``data.series[].tags`` and maps them to the corresponding record fields.

Curl example
------------

Post a representative payload with one series:

.. code-block:: console

   $ curl -s -X POST https://<snooze-host>/api/v1/webhook/kapacitor \
       -H 'Content-Type: application/json' \
       -d '{
         "id": "cpu_alert/web-1",
         "message": "CPU critical on web-1: CRITICAL",
         "details": "",
         "time": "2024-01-15T12:00:00Z",
         "duration": 0,
         "level": "CRITICAL",
         "data": {
           "series": [
             {
               "name": "cpu",
               "tags": {
                 "host": "web-1",
                 "process": "nginx",
                 "env": "prod"
               },
               "columns": ["time", "usage_idle"],
               "values": [
                 ["2024-01-15T12:00:00Z", 7.3]
               ]
             }
           ]
         },
         "tags": {"datacenter": "eu-west-1"}
       }'

Expected response::

   {"accepted":1,"received":1,"status":"ok"}

To test a recovery (``level=OK`` sets ``State="close"``):

.. code-block:: console

   $ curl -s -X POST https://<snooze-host>/api/v1/webhook/kapacitor \
       -H 'Content-Type: application/json' \
       -d '{
         "id": "cpu_alert/web-1",
         "message": "CPU recovered on web-1",
         "time": "2024-01-15T13:00:00Z",
         "level": "OK",
         "data": {
           "series": [
             {
               "name": "cpu",
               "tags": {"host": "web-1", "process": "nginx"}
             }
           ]
         }
       }'

Expected response::

   {"accepted":1,"received":1,"status":"ok"}

Field mapping
-------------

One Snooze record is produced per ``data.series[]`` entry.  The ``severity``,
``host``, and ``process`` tag keys are popped from the series tags before the
remaining keys are collected into ``Record.Tags``.

.. list-table::
   :header-rows: 1
   :widths: 25 20 55

   * - Payload field
     - Snooze field
     - Notes
   * - ``Source`` (constant)
     - ``Source``
     - Always ``"kapacitor"``.
   * - ``data.series[i].tags.host``
     - ``Host``
     - Falls back to ``data.series[i].tags.instance``. Empty string when
       neither is set.
   * - ``data.series[i].tags.severity``
     - ``Severity``
     - When present, used verbatim (takes priority over ``level``).
       Otherwise the envelope ``level`` is normalised:
       ``CRITICAL`` → ``"critical"``; ``WARNING`` → ``"warning"``;
       ``INFO`` or ``OK`` → ``"info"``. Unknown levels are lowercased and
       passed through. Defaults to ``"critical"`` when ``level`` is absent.
   * - ``message``
     - ``Message``
     - The human-readable alert message from the Kapacitor TICKscript.
   * - ``data.series[i].tags.process``
     - ``Process``
     - Falls back to the envelope ``id`` when the tag is absent.
   * - ``time``
     - ``Timestamp``
     - Falls back to server ``time.Now()`` when absent or zero.
   * - ``data.series[i].tags`` (remaining, after popping host/process/severity)
       + ``tags`` (envelope)
     - ``Tags``
     - Sorted, deduplicated union of the remaining series tag keys and the
       envelope ``tags`` keys.
   * - ``level``
     - ``State``
     - ``"OK"`` (after upper-casing) → ``State="close"``; all other levels
       leave ``State`` empty.
   * - Full envelope
     - ``Raw``
     - The complete decoded payload is JSON-round-tripped into ``Raw``,
       including ``data.series`` with all columns and values.

Notes & limitations
===================

- **Unauthenticated by default.** The endpoint accepts any POST from any
  source. Restrict access at the network layer and/or configure a shared
  ingest token — see ``config.ingest`` / :doc:`../../configuration/ingest`
  and :ref:`integrations`.
- **Series fan-out.** A Kapacitor batch alert that matches multiple hosts
  produces one Snooze record per series. Each record carries only its own
  series data in ``Raw``, not the full batch.
- **Keepalive payloads.** Kapacitor periodically sends payloads with an empty
  ``data.series`` array to confirm liveness. These are acknowledged with HTTP
  200 and ``{"received":0}`` and do not create records.
- **Tag-based metadata.** Host, process, and severity must be exposed as
  InfluxDB measurement tags or computed in the TICKscript and attached to the
  series data. Without a ``host`` tag, the ``Host`` field is left empty.
- **Level normalisation vs. severity tag.** The per-series ``severity`` tag
  always wins over the envelope ``level``. Use this to override the Kapacitor
  level vocabulary (CRITICAL/WARNING/INFO/OK) with a Snooze-vocabulary value
  for a specific metric.
