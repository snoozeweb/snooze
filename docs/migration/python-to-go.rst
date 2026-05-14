.. _migration-python-to-go:

================================
Python 1.x → Go 2.0 migration
================================

This page is the Sphinx-rendered companion to
``docs/migration/python-to-go.md``, which is the canonical source. The
Markdown file is the one to edit; this RST view exists so the page
shows up in the rendered HTML tree without requiring ``myst-parser``.

For the full table-driven mapping (configuration keys, API endpoints,
CLI subcommands, root rotation, Mongo collection drops), open
:download:`python-to-go.md <python-to-go.md>` or read it on GitHub at
`docs/migration/python-to-go.md
<https://github.com/snoozeweb/snooze/blob/master/docs/migration/python-to-go.md>`_.

Short summary
=============

* **Authorization** header is now ``Bearer`` rather than ``JWT``.
* **List responses** are wrapped in ``{data, meta}`` and the positional
  ``/{search}/{perpage}/{pagenb}/{orderby}/{asc}`` URL shape is replaced
  by ``?q=<base64url-json>&offset&limit&orderby&asc``.
* **Config hot-reload of YAML is gone.** Runtime-mutable settings live
  in the ``settings`` collection in the database and are reachable
  through the regular REST CRUD surface (or the WebUI).
* **Bootstrap root password** is a 24-byte random secret, bcrypt-hashed
  and printed once to stderr on the first boot. ``root:root`` no longer
  exists by default.
* **SQLite** is now first-class via ``modernc.org/sqlite`` (pure Go,
  JSON1). The legacy ``database.type: file`` alias maps to SQLite.
* **Kombu / amqp-on-mongo is retired.** Drop the ``snooze_kombu_*``
  Mongo collections; they are not used.
* **CLI**: ``snooze-server`` is the daemon, ``snooze`` is the read/write
  client, plus eight component daemons (``snooze-relp``,
  ``snooze-syslog``, ``snooze-snmptrap``, ``snooze-smtp``,
  ``snooze-mattermost``, ``snooze-googlechat``, ``snooze-teams``,
  ``snooze-pacemaker``).

See the Markdown file for the full per-section tables.

.. note::

   TODO: this RST stub will be replaced by a ``myst-parser``-backed
   include once ``docs/conf.py`` adds the extension. For now Sphinx
   can build the tree without the Markdown plugin while operators
   keep reading the Markdown directly.
