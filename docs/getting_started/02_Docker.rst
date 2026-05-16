.. _docker:

=================
Docker deployment
=================

Snooze 2.0 publishes one distroless image per Go binary at
``snoozeweb/snooze:latest`` (server) or ``snoozeweb/snooze-<binary>:latest`` (components). The most common starting
points are documented below; the repository ``docker-compose.yaml``
covers each layout end-to-end with named profiles.

Single container, SQLite
========================

.. code-block:: console

    $ docker run --name snooze -d -p 5200:5200 \
        -e SNOOZE_DATABASE_TYPE=sqlite \
        -e SNOOZE_DATABASE_PATH=/var/lib/snooze/db.sqlite \
        -v snooze-data:/var/lib/snooze \
        snoozeweb/snooze:latest

The Web interface is then available at ``http://localhost:5200``. This
is the lowest-friction layout: no external database, no replica set,
zero infrastructure.

Single container, MongoDB
=========================

.. code-block:: console

    $ docker run --name snooze-db -d mongo:7
    $ docker run --name snooze -d -p 5200:5200 \
        -e DATABASE_URL=mongodb://snooze-db:27017/snooze \
        --link snooze-db:snooze-db \
        snoozeweb/snooze:latest

Single container, PostgreSQL
============================

.. code-block:: console

    $ docker run --name snooze-pg -d \
        -e POSTGRES_DB=snooze \
        -e POSTGRES_USER=snooze \
        -e POSTGRES_PASSWORD=snooze \
        postgres:16-alpine
    $ docker run --name snooze -d -p 5200:5200 \
        -e DATABASE_URL=postgresql://snooze:snooze@snooze-pg:5432/snooze \
        --link snooze-pg:snooze-pg \
        snoozeweb/snooze:latest

Compose stack
=============

The repository ``docker-compose.yaml`` is profiled so you can pick a
backend at ``up`` time:

.. code-block:: console

    $ docker compose --profile sqlite   up   # single replica + named volume
    $ docker compose --profile mongo    up   # 3-node RS + nginx LB on :80
    $ docker compose --profile postgres up   # single Postgres + snooze on :5210

The ``mongo`` profile is the canonical multi-replica layout: three
``snooze-server`` containers behind an nginx ``round_robin`` load
balancer talking to a 3-node replica set, with the bus / syncer
piggy-backed on Mongo change streams.

Helm
====

A Helm chart lives at ``packaging/helm/``. Set ``database.kind`` to
pick the backend:

.. code-block:: console

    $ helm install snooze packaging/helm \
        --set database.kind=postgres   # provisions a CNPG cluster
    $ helm install snooze packaging/helm \
        --set database.kind=mongo      # provisions a MongoDBCommunity RS
    $ helm install snooze packaging/helm \
        --set database.kind=sqlite     # StatefulSet + PVC
