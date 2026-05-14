.. _getting-started-go:

==========================
Getting started (Go, v2.0)
==========================

Snooze 2.0 ships ten statically-linked Go binaries. The fastest path
from clone to running server is ``docker compose``; for a host install
the ``.deb`` / ``.rpm`` packages drop a systemd unit and an empty
config directory under ``/etc/snooze/server-go``.

Run from docker-compose
=======================

The repository ships a ``docker-compose.yaml`` with three profiles —
exactly one should be active per ``docker compose up`` invocation.

SQLite (zero deps, single replica):

.. code-block:: console

   $ docker compose --profile sqlite up
   $ open http://localhost:5220

MongoDB (3-node replica set + nginx load balancer on ``:80``):

.. code-block:: console

   $ docker compose --profile mongo up
   $ open http://localhost/

PostgreSQL (single instance, snooze-server published on ``:5210``):

.. code-block:: console

   $ docker compose --profile postgres up
   $ open http://localhost:5210

In every profile the bootstrap root password is printed once to the
``snooze-server`` container stderr the first time it boots — copy it
out of the logs and rotate it (see :ref:`migration-python-to-go`).

Run from a Linux package
========================

Debian / Ubuntu:

.. code-block:: console

   $ wget https://deb.snoozeweb.net/snooze-server_2.0.0_amd64.deb
   $ sudo apt install ./snooze-server_2.0.0_amd64.deb
   $ sudo systemctl start snooze-server
   $ sudo journalctl -u snooze-server | grep 'bootstrap: root password'

RHEL / CentOS / Rocky / Alma:

.. code-block:: console

   $ wget https://rpm.snoozeweb.net/snooze-server-2.0.0.x86_64.rpm
   $ sudo dnf localinstall snooze-server-2.0.0.x86_64.rpm
   $ sudo systemctl start snooze-server
   $ sudo journalctl -u snooze-server | grep 'bootstrap: root password'

The default install uses SQLite at ``/var/lib/snooze/db.sqlite`` and
listens on ``0.0.0.0:5200``. To switch to MongoDB or Postgres, edit
``/etc/snooze/server-go/core.yaml`` (see :doc:`/configuration/core`) and
restart the service.

Server config: bootstrap YAML
=============================

A minimal ``/etc/snooze/server-go/core.yaml``:

.. code-block:: yaml

   listen_addr: "0.0.0.0"
   port: 5200
   bootstrap_db: true
   create_root_user: true

   database:
     type: sqlite           # one of: sqlite, mongo, postgres
     path: /var/lib/snooze/db.sqlite

   ssl:
     enabled: false

   cors:
     allow_origins: "*"
     allow_credentials: "*"

For MongoDB:

.. code-block:: yaml

   database:
     type: mongo
     url: mongodb://mongo1:27017,mongo2:27017,mongo3:27017/snooze?replicaSet=rs0

For Postgres:

.. code-block:: yaml

   database:
     type: postgres
     dsn: postgresql://snooze:snooze@postgres:5432/snooze
     pool_min_size: 2
     pool_max_size: 10

Every section has its own file:

* ``core.yaml`` — listen address, database, SSL, CORS, backup.
* ``general.yaml`` — auth default, anonymous toggle, OK severities.
* ``ldap.yaml`` — LDAP bind details (only read when ``enabled: true``).
* ``housekeeper.yaml`` — TTLs and cleanup intervals.
* ``notification.yaml`` — retry / frequency defaults.
* ``syncer.yaml`` — heartbeat interval for the cluster syncer.
* ``web.yaml`` — embedded UI toggle and path.
* ``auth.yaml`` — JWT signing secret, algorithm, lease.

Environment variable overrides follow ``SNOOZE_<SECTION>_<KEY>``;
``DATABASE_URL`` continues to be the flat shortcut for ``database``.

CLI: the ``snooze`` client
==========================

The ``snooze`` binary is a separate client that authenticates to
``snooze-server`` over HTTP. With a JWT in ``$SNOOZE_TOKEN`` and
``$SNOOZE_URL`` pointing at the server, you can list, fetch, and edit
any plugin's collection from the shell. See ``snooze --help``.

Server bring-up checks
======================

.. code-block:: console

   $ curl -s http://localhost:5200/healthz       # liveness
   {"status":"ok"}
   $ curl -s http://localhost:5200/readyz        # readiness (DB poll)
   {"status":"ok"}
   $ curl -s http://localhost:5200/metrics | head
   # HELP go_gc_duration_seconds A summary of the GC pause durations…

For the verbose per-subsystem health view (DB, plugins, syncer):

.. code-block:: console

   $ curl -s http://localhost:5200/api/v1/health | jq .

Logging in
==========

Local password login:

.. code-block:: console

   $ curl -X POST -H 'Content-Type: application/json' \
          -d '{"username":"root","password":"<bootstrap-password>"}' \
          http://localhost:5200/api/v1/login/local
   {"token":"eyJhbGciOi…","expires_at":"2026-05-14T10:30:00Z","method":"local"}

The returned bearer token is what every subsequent API call should
send in ``Authorization: Bearer …``.

LDAP and anonymous backends are wired up the same way; the path is
``/api/v1/login/ldap`` or ``/api/v1/login/anonymous``.

Where to go next
================

* :doc:`/configuration/index` — every configuration knob, by file.
* :doc:`/migration/index` — moving from a Python 1.x install.
* ``api/openapi.yaml`` — machine-readable v1 API spec.
