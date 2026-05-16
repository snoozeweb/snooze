.. SnoozeWeb documentation master file.

=========
SnoozeWeb
=========

.. note::

   **v2.0 is the Go rewrite.** The Python codebase is retired. See the
   :doc:`migration/index` for the upgrade path and the project
   `CHANGELOG <https://github.com/snoozeweb/snooze/blob/master/CHANGELOG.md>`_
   for the breaking-change inventory.

SnoozeWeb is a monitoring tool used for log aggregation and alerting. It
ingests alerts from many sources (syslog, SNMP traps, RELP, AlertManager,
Grafana, InfluxDB 2, Kapacitor, Prometheus, custom webhooks), applies
rule/aggregate/snooze/notification pipelines, and ships outbound
notifications to mail, chat (Mattermost, Teams, Google Chat), webhooks,
or custom action plugins.

Features
========

* Single-binary Go server (`snooze-server`) plus a separate `snooze`
  CLI and eight optional auxiliary daemons (`snooze-relp`,
  `snooze-syslog`, `snooze-snmptrap`, `snooze-smtp`, `snooze-mattermost`,
  `snooze-googlechat`, `snooze-teams`, `snooze-pacemaker`).
* Three pluggable backends: SQLite (zero-deps default), MongoDB
  (production with replica sets), Postgres (production with
  CloudNativePG / managed services).
* Three message buses, matched to the backend (in-process channel,
  Mongo change streams, Postgres LISTEN/NOTIFY).
* Local + LDAP + anonymous authentication. Bearer-JWT API tokens.
* OpenAPI 3.1 specification at ``/api/openapi.yaml``.
* Structured ``log/slog`` JSON logs, OpenTelemetry traces, a Prometheus
  registry served at ``/metrics``.
* Distroless container images at ``snoozeweb/snooze-<binary>``.
* Helm chart with ``database.kind: mongo | postgres | sqlite``.

.. figure:: images/web_alerts.png
    :align: center
    :target: _images/web_alerts.png

Demo
====

Try it at https://try.snoozeweb.net.

Quick start
===========

.. code-block:: console

   # Local docker-compose with SQLite (single-replica)
   $ docker compose --profile sqlite up

   # …or with a 3-node MongoDB replica set + nginx load balancer
   $ docker compose --profile mongo up

   # …or with a single Postgres instance
   $ docker compose --profile postgres up

The web UI listens on ``http://localhost:5200`` (or ``:80`` behind the
nginx in the ``mongo`` profile). The bootstrap root password is printed
once on the server stderr; see
:doc:`migration/python-to-go` for rotation.

Contribute
==========

* Repository: https://github.com/snoozeweb/snooze
* Issue tracker: https://github.com/snoozeweb/snooze/issues

License
=======

Snooze is licensed under the GNU Affero General Public License v3.0 or
later. See the ``LICENSE`` file in the repository root for the full
text.

.. toctree::
   :maxdepth: 2
   :hidden:

   getting_started/index
   configuration/index
   migration/index
   general/index
