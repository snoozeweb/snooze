.. _Syncer-configuration:

####################
Syncer configuration
####################


Config for the housekeeper thread. Can be edited live in the web interface.
Usually located at `/etc/snooze/server/housekeeper.yaml`.

**********
Properties
**********

hostname
========

    :Type: string

    An override for the hostname of the node in the cluster. Shouldbe different for each node



total
=====

    :Type: integer
    :Default: ``1``

    Total number of nodes the syncer should expect (for status reporting)



sync_interval_ms
================

    :Type: integer
    :Default: ``1000``

    Interval between checks to update the in-memory value




