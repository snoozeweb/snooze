.. _configuration:

=============
Configuration
=============

The only configuration file not managed in the web interface is ``/etc/snooze/server/core.yaml`` and requires restarting Snooze if changed.

.. glossary::

    listen_addr (``'0.0.0.0'``)
        IPv4 address on which Snooze process is listening to

    port (``5200``)
        Port on which Snooze process is listening to

    debug (``false``)
        Activate debug log output

    bootstrap_db (``true``)
        Populate the database with an initial configuration

    create_root_user (``true``)
        Create a **root** user with a default password **root**

    no_login (``false``)
        Disable Authentication (everyone has admin priviledges)

    audit_excluded_paths (``[/api/patlite, /metrics, /web]``)
        List of HTTP paths excluded from audit logs

    ssl
        enabled (``false``)
            Enable TLS termination for both the API and the web interface
        certfile (``''``)
            Path to the SSL certificate
        keyfile (``''``)
            Path to the private key

    web
        enabled (``true``)
            Enable the web interface
        path (``/opt/snooze/web``)
            Path to the web interface dist files

    clustering
        enable` (``false``)
            Enable clustering mode
        members
            List of snooze servers in the cluster {host, port}

            - :host (``localhost``): Hostname or IPv4 address of the first member
              :port (``5200``): Port on which the first member is listening to
            - ...

    database
        type (``file``)
            Backend database to use (file or mongo)

    backup
        enabled (``true``)
            Enable backups

    path (``WORKDIR/backups``)
        Path to store database backups

    exclude (``[record, stats, comment, secrets]``)
        Collections to exclude from backups

Example:
========

.. code-block:: yaml
    :caption: MongoDB backend with database replication enabled

    database:
        type: mongo
        host:
            - hostA
            - hostB
            - hostC
        port: 27017
        username: snooze
        password: 7dg9khqg1w6
        authSource: snooze
        replicaSet: rs0
