Configuration
=============

.. _configuration:

Snooze 2.0 uses a **two-tier** configuration model:

1. **Bootstrap YAML** under ``/etc/snooze/server-go/`` (one file per
   schema section). Read once at process start; a restart is required
   for changes. The schema is defined in
   ``internal/config/schema/*.go``.
2. **Runtime settings** stored in the database via the new
   ``settings`` plugin. Mutable at runtime through ``PATCH
   /api/v1/settings/{key}`` (or the WebUI). Changes propagate across
   replicas via the backend-native syncer (Postgres ``LISTEN/NOTIFY``,
   Mongo change streams, in-process channel for SQLite).

The hot-reload of YAML files used by Python 1.x's ``WritableConfig``
is **gone**. Anything that needs to change without a restart belongs
in the ``settings`` plugin.

Bootstrap YAML files
====================

.. toctree::
    :titlesonly:

    core
    general
    ldap_auth
    housekeeping
    notifications
    syncer
    sqlite
    postgres

Each file is optional: the corresponding Go ``Default*`` constructor
fills in sensible defaults. See ``internal/config/load.go`` for the
discovery order (lower-numbered overrides win):

1. ``/etc/snooze/server-go/<section>.yaml``
2. ``/etc/snooze/server/<section>.yaml`` *(legacy Python directory,
   accepted unchanged)*
3. environment variables: ``SNOOZE_<SECTION>_<KEY>``
4. ``DATABASE_URL`` flat shortcut

Runtime-mutable settings
========================

The ``settings`` plugin exposes the same REST CRUD shape as every
other plugin:

* ``GET /api/v1/settings`` — list every key.
* ``GET /api/v1/settings/{key}`` — fetch one.
* ``PATCH /api/v1/settings/{key}`` — update the value.

Web interface
=============

The WebUI ships the same Settings panel; the form submits to the
``settings`` plugin REST endpoints rather than rewriting YAML on disk.

.. figure:: images/web_settings_general.png
    :align: center
