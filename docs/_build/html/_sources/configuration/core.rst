.. _Core-configuration:

##################
Core configuration
##################

    :Package location: ``/etc/snooze/server/core.yaml``
    :Live reload: ``False``

Core configuration. Not editable live. Require a restart of the server.

**********
Properties
**********

listen_addr
===========

    :Type: string (ipvanyaddress)
    :Default: ``'0.0.0.0'``

    IPv4 address on which Snooze process is listening to



port
====

    :Type: integer
    :Default: ``5200``

    Port on which Snooze process is listening to



bootstrap_db
============

    :Type: boolean
    :Default: ``True``

    Populate the database with an initial configuration



unix_socket
===========

    :Type: string (path)
    :Default: ``'/var/run/snooze/server.socket'``

    Listen on this unix socket to issue root tokens



no_login
========

    :Type: boolean
    :Environment variable: ``SNOOZE_NO_LOGIN``
    :Default: ``False``

    Disable Authentication (everyone has admin priviledges)



audit_excluded_paths
====================

    :Type: array[string]
    :Default: ``['/api/patlite', '/metrics', '/web']``

    A list of HTTP paths excluded from audit logs. Any paththat starts with a path in this list will be excluded.



process_plugins
===============

    :Type: array[string]
    :Default: ``['rule', 'aggregaterule', 'snooze', 'notification']``

    List of plugins that will be used for processing alerts. Order matters.



database
========

    :Type: :ref:`MongodbConfig<MongodbConfig>` | :ref:`FileConfig<FileConfig>`
    :Environment variable: ``DATABASE_URL``



init_sleep
==========

    :Type: integer
    :Default: ``5``

    Time to sleep before retrying certain operations (bootstrap, ...)



create_root_user
================

    :Type: boolean
    :Default: ``True``

    Create a *root* user with a default password *root*



ssl
===

    :Type: :ref:`SslConfig<SslConfig>`



web
===

    :Type: :ref:`WebConfig<WebConfig>`



backup
======

    :Type: :ref:`BackupConfig<BackupConfig>`



cors
====

    :Type: :ref:`CorsConfig<CorsConfig>`




***********
Definitions
***********

.. _MongodbConfig:

MongodbConfig
=============

Mongodb configuration passed to pymongo MongoClient

type
----

    :Type: 'mongo'
    :Default: ``'mongo'``



host
----

    :Type: string | array[string]

    Hostname or IP address or Unix domain socket path of a single mongod or mongos instanceto connect to



port
----

    :Type: integer

    Port number on which to connect





.. _FileConfig:

FileConfig
==========

type
----

    :Type: 'file'
    :Default: ``'file'``



path
----

    :Type: string (path)
    :Default: ``'db.json'``





.. _SslConfig:

SslConfig
=========

SSL configuration

enabled
-------

    :Type: boolean
    :Default: ``False``

    Enabling TLS termination



certfile
--------

    :Type: string (path)
    :Environment variable: ``SNOOZE_CERT_FILE``

    Path to the x509 PEM style certificate to use for TLS termination

    .. admonition:: Example 1

        .. code-block:: yaml

            certfile: /etc/pki/tls/certs/snooze.crt

    .. admonition:: Example 2

        .. code-block:: yaml

            certfile: /etc/ssl/certs/snooze.crt



keyfile
-------

    :Type: string (path)
    :Environment variable: ``SNOOZE_KEY_FILE``

    Path to the private key to use for TLS termination

    .. admonition:: Example 1

        .. code-block:: yaml

            keyfile: /etc/pki/tls/private/snooze.key

    .. admonition:: Example 2

        .. code-block:: yaml

            keyfile: /etc/ssl/private/snooze.key





.. _WebConfig:

WebConfig
=========

The subconfig for the web server (snooze-web)

enabled
-------

    :Type: boolean
    :Default: ``True``

    Enable the web interface



path
----

    :Type: string (path)
    :Default: ``'/opt/snooze/web'``

    Path to the web interface dist files





.. _BackupConfig:

BackupConfig
============

Configuration for the backup job

enabled
-------

    :Type: boolean
    :Default: ``True``

    Enable backups



path
----

    :Type: string (path)
    :Environment variable: ``SNOOZE_BACKUP_PATH``
    :Default: ``'/var/lib/snooze'``

    Path to store database backups



excludes
--------

    :Type: array[string]
    :Default: ``['record', 'stats', 'comment', 'secrets', 'aggregate', 'system.profile']``

    Collections to exclude from backups





.. _CorsConfig:

CorsConfig
==========

CORS configuration for the web server

allow_origins
-------------

    :Type: string
    :Default: ``'*'``



allow_credentials
-----------------

    :Type: string
    :Default: ``'*'``







