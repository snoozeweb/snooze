.. _clustering:

==========
Clustering
==========

Overview
--------

To maintain a high availabilty of Snooze server service, it is necessary to have multiple nodes running in a cluster.

Clustering offers a transparent way for the user to replicate all changes done one node's configuration file to all other nodes in the cluster.

.. caution::

    It is important to not mix up Service HA (snooze-server) and Data HA (database, mongodb). This document is
    only covering Service HA.

Configuration
-------------

Clustering configuration should be defined in ``/etc/snooze/server/core.yaml`` and requires restarting Snooze if changed.

**clustering**:

:enable` (``false``): Enable clustering mode
:members: List of snooze servers in the cluster {host, port}

- :host (``localhost``): Hostname or IPv4 address of the first member
  :port (``5200``): Port on which the first member is listening to
- ...

Web interface
-------------

.. figure:: images/web_cluster.png
    :align: center
