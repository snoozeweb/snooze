.. _PostgreSQL-backend:

##################
PostgreSQL backend
##################

Snooze ships a PostgreSQL backend alongside the default MongoDB one. It
stores every collection as a per-collection ``snooze_<name>`` table
with a single ``jsonb`` data column, so the schemaless plugin contract
is preserved — adding a plugin never requires a SQL migration.

The field-level reference for ``PostgresConfig`` is auto-generated and
lives under :ref:`Core-configuration` (in the *Definitions* section).
This page is the narrative companion.

************
Installation
************

The driver is opt-in to keep the default install lean:

.. code-block:: bash

    uv sync --extra postgres

The extra installs ``psycopg[binary,pool]>=3.2,<4``.

*************
Configuration
*************

Set ``database.type`` to ``postgres`` in ``core.yaml``. Either provide
a full ``dsn`` libpq connection string, or fill in the decomposed
fields. Anything left unset falls back to the standard ``PG*``
environment variables (``PGHOST``, ``PGPORT``, ``PGDATABASE``,
``PGUSER``, ``PGPASSWORD``, ``PGSSLMODE``).

.. code-block:: yaml

    database:
        type: postgres
        host: localhost
        port: 5432
        database: snooze
        user: snooze
        password: snooze
        sslmode: prefer
        pool_min_size: 1
        pool_max_size: 10

A complete example also lives at ``examples/postgres.yaml``.

*****************
When to choose it
*****************

Pick PostgreSQL over MongoDB when:

* Your platform already operates Postgres and you'd prefer not to run
  a second stateful service.
* You want an unambiguously OSI-approved licence on your database (the
  MongoDB Server-Side Public License isn't).
* You need standard SQL tooling for backup, monitoring, and audit.

Stay on MongoDB when you need the multi-instance kombu message queue
(``MongodbTransport``) for cross-process plugin sync. The Postgres
backend currently falls back to the in-memory kombu transport —
single-process only — and the DB-level syncer keeps instances in sync
regardless.

*****************
Schema and shape
*****************

Each logical collection becomes a table:

.. code-block:: sql

    CREATE TABLE snooze_<collection> (
        uid         TEXT PRIMARY KEY,
        data        JSONB NOT NULL,
        seq         BIGSERIAL NOT NULL,
        updated_at  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
    );
    CREATE INDEX idx_<table>_data_gin   ON <table> USING GIN (data jsonb_path_ops);
    CREATE INDEX idx_<table>_updated_at ON <table> (updated_at);

Tables are created lazily on first write to a collection, mirroring
MongoDB's implicit collection creation. Collection names containing
dots (e.g. ``user.password``) map to ``snooze_user__password`` on the
SQL side.

*******
Sizing
*******

The default GIN index on ``data jsonb_path_ops`` accelerates exact
containment queries. For very large collections, add B-tree indexes on
frequently-queried fields:

.. code-block:: sql

    CREATE INDEX ON snooze_record ((data->>'host'));

The connection pool defaults to ``min_size=1, max_size=10``. Raise
``pool_max_size`` for high-concurrency setups; one connection is held
per active request thread.

*****
Notes
*****

* ``MATCHES`` and ``SEARCH`` use POSIX regex (``~*``) for
  case-insensitive matching. No external extensions (``pg_trgm`` /
  ``pgvector``) are required.
* MongoDB's ``$where`` JavaScript predicate has no Postgres equivalent
  and is not used; ``SEARCH`` falls back to a regex over the whole
  serialised document when no explicit ``search_fields`` are
  registered for a collection.
* Snooze-side backups (``BackupConfig``) dump each table as a JSON file
  under ``backup.path``; use ``pg_dump`` if you want SQL-level dumps.
* The Helm chart provisions Postgres via the `CloudNativePG
  <https://cloudnative-pg.io/>`_ operator (the operator itself must be
  installed in the cluster — the chart ships only the ``Cluster`` CR).
  Switch it on with ``--set database.kind=postgres``; tune the cluster
  via the ``postgres.*`` values (instances, image, storage). See
  ``packaging/helm/values.yaml`` for the full surface.
