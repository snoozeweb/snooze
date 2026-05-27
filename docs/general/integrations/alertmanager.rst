.. _integration-alertmanager:

=====================================
Prometheus Alertmanager (input)
=====================================

Overview
========

The **alertmanager** plugin is an in-process WebhookReceiver that accepts the
`Prometheus Alertmanager v4 webhook payload
<https://prometheus.io/docs/alerting/latest/configuration/#webhook_config>`_
and converts each alert in the ``alerts[]`` array into a Snooze record. It is
registered at ``/api/v1/webhook/alertmanager``.

Alertmanager delivers firing and resolved alerts by POSTing a JSON envelope to
a user-configured webhook URL. The plugin maps labels and annotations to the
standard Snooze record schema, strips trailing port suffixes from instance
labels, and emits automatic resolution records (``State: "close"``) for
``resolved`` status alerts.

.. note::

   This plugin targets the explicit AlertManager v4 receiver path with stricter
   defaults: the ``Source`` constant is ``"AlertManager"`` (capital A and M),
   the host-not-found fallback is ``"-"``, and the process label search order
   is extended to include ``alertgroup`` and ``job``. If you need the historical
   ``"prometheus"`` source label and a simpler fallback chain, use the
   :ref:`integration-prometheus` plugin instead.

Configuration
=============

Inbound URL
-----------

The plugin mounts at::

    /api/v1/webhook/alertmanager

No authentication is required on this endpoint by default. See
:ref:`integrations` and :doc:`../../configuration/ingest` for hardening
options including a shared ingest token (``config.ingest.token``).

Pointing Alertmanager at Snooze
---------------------------------

Add a ``webhook_config`` receiver to your Alertmanager configuration:

.. code-block:: yaml
   :caption: alertmanager.yml

   receivers:
     - name: snooze
       webhook_configs:
         - url: 'https://<snooze-host>/api/v1/webhook/alertmanager'
           send_resolved: true

   route:
     receiver: snooze

Set ``send_resolved: true`` so that resolved alerts generate a ``State="close"``
record in Snooze and open alerts are automatically closed.

Curl example
------------

Post a minimal two-alert payload:

.. code-block:: console

   $ curl -s -X POST https://<snooze-host>/api/v1/webhook/alertmanager \
       -H 'Content-Type: application/json' \
       -d '{
         "version": "4",
         "groupKey": "{}:{alertname=\"HighCPU\"}",
         "status": "firing",
         "receiver": "snooze",
         "groupLabels": {"alertname": "HighCPU"},
         "commonLabels": {"env": "prod"},
         "commonAnnotations": {},
         "externalURL": "https://alertmanager.example.com",
         "alerts": [
           {
             "status": "firing",
             "labels": {
               "alertname": "HighCPU",
               "instance": "web-1.local:9100",
               "severity": "critical",
               "job": "node_exporter",
               "env": "prod",
               "tags": "team-ops,priority-high"
             },
             "annotations": {
               "summary": "CPU usage above 90% on web-1"
             },
             "startsAt": "2024-01-15T12:00:00Z",
             "endsAt": "0001-01-01T00:00:00Z",
             "generatorURL": "https://prometheus.example.com/graph",
             "fingerprint": "abc123def456"
           }
         ]
       }'

Expected response::

   {"accepted":1,"received":1,"status":"ok"}

Field mapping
-------------

One Snooze record is produced per entry in the ``alerts[]`` array.  Labels are
merged in the order ``commonLabels`` → ``groupLabels`` → ``alert.labels``, with
later values winning.  Annotation keys that contain dots are sanitised
(``"."`` replaced with ``"_"``) before storage to avoid MongoDB path
collisions.

.. list-table::
   :header-rows: 1
   :widths: 25 20 55

   * - Payload field
     - Snooze field
     - Notes
   * - ``Source`` (constant)
     - ``Source``
     - Always ``"AlertManager"`` (capital A, capital M).
   * - ``alert.labels.host``
     - ``Host``
     - Falls back to ``labels.instance`` (with trailing ``:port`` stripped),
       then ``labels.exported_instance``. Defaults to ``"-"`` when none of
       the candidates are set.
   * - ``alert.labels.severity``
     - ``Severity``
     - For ``status=firing``: uses this label value; defaults to
       ``"critical"`` when absent. For ``status=resolved``: always ``"ok"``.
   * - ``alert.annotations.message``
     - ``Message``
     - Falls back to ``annotations.summary`` → ``annotations.description`` →
       ``annotations.externalURL``.
   * - ``alert.labels.process``
     - ``Process``
     - Falls back to ``labels.service`` → ``labels.alertgroup`` →
       ``labels.job``. Defaults to ``"-"`` when none are set.
   * - ``alert.labels.tags``
     - ``Tags``
     - Split on commas and whitespace into a string array. Absent when the
       ``tags`` label is not present.
   * - ``alert.startsAt``
     - ``Timestamp``
     - Falls back to server ``time.Now()`` when absent or zero.
   * - ``alert.status``
     - ``State``
     - ``"resolved"`` (case-insensitive) → ``State="close"``; all other
       values leave ``State`` empty.
   * - ``alert.labels``, ``alert.annotations``, ``generatorURL``,
       ``externalURL``, ``alert.status``, ``alert.fingerprint``
     - ``Extra`` (top-level on wire)
     - These fields are folded into the record document at the top level by
       the pipeline projector.

Notes & limitations
===================

- **Unauthenticated by default.** The endpoint accepts any POST from any
  source. Place Snooze behind a reverse proxy restricted to your Alertmanager
  IP(s) and/or configure a shared ingest token — see ``config.ingest`` /
  :doc:`../../configuration/ingest` and :ref:`integrations`.
- **One record per alert.** A single webhook POST can carry many alerts;
  each becomes an independent Snooze record.
- **Port stripping.** Prometheus instance labels frequently carry a scrape port
  (e.g. ``"node-1:9100"``). This plugin strips the trailing ``:NNN`` before
  populating ``Host``, producing ``"node-1"`` instead.
- **Dot sanitisation.** Annotation keys with dots are rewritten
  (``"some.key"`` → ``"some_key"``) in ``Extra.annotations`` to prevent
  document path conflicts. Label keys are not affected because Prometheus
  labels cannot contain dots.
- **Resolution pairing.** The ``"close"`` record carries the same
  ``labels.host`` / ``labels.job`` values as the firing record. Configure
  aggregation rules on ``Extra.fingerprint`` or the alertname label for
  reliable deduplication.
