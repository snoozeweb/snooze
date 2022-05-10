.. _metrics:

=======
Metrics
=======

Overview
========

SnoozeWeb has several metrics exposed by the HTTP endpoint ``/metrics`` in `OpenMetrics <https://openmetrics.io/>`_ format.

:process_alert_duration: Average time spend processing a alert by ``source``, ``environment`` and ``severity``.
:alert_hit: Counter of received alerts by by ``source``, ``environment`` and ``severity``.
:alert_snoozed: Counter of snoozed alerts by ``name``.
:alert_throttled: Counter of throttled alerts by ``name``.
:alert_closed: Counter of received closed alerts by ``name``.
:notification_sent: Counter of notification sent by ``name``.
:action_success: Counter of action that succeeded by ``name``.
:action_error: Counter of action that failed by ``name``.

Web interface
=============

Snooze web interface has a few built-in charts displaying these metrics under its dashboard section.

Alerts
------

.. figure:: images/web_dashboard.png
    :align: center

The time interval for displaying the metrics can be changed freely. It has a few presets and defaults to **daily**.

.. hint::

    Clicking on a point of the chart will redirect to the alert section showing only alerts during around this period.

    Clicking on a label will filter it in/out.

    Changing the time interval will also affect all other charts time inteval as well.


Other charts
------------

A few other charts are also computed:

* Alerts by Source
* Alerts by Environment
* Actions
* Throttled Alerts
* Snoozed Alerts
* Alerts by Weekday
