.. _General-configuration:

#####################
General configuration
#####################

    :Package location: ``/etc/snooze/server/general.yaml``
    :Live reload: ``True``

General configuration of snooze. Can be edited live in the web interface.
Usually located at `/etc/snooze/server/general.yaml`.

**********
Properties
**********

default_auth_backend
====================

    :Type: 'local' | 'ldap' | 'anonymous'
    :Default: ``'local'``

    Backend that will be first in the list of displayed authentication backends



local_users_enabled
===================

    :Type: boolean
    :Default: ``True``

    Enable the creation of local users in snooze. This can be disabled when another reliable authentication backend is used, and the admin want to make auditing easier



metrics_enabled
===============

    :Type: boolean
    :Default: ``True``

    Enable Prometheus metrics



anonymous_enabled
=================

    :Type: boolean
    :Default: ``False``

    Enable anonymous user login. When a user log in as anonymous, he will be given user permissions



ok_severities
=============

    :Type: array[string]
    :Default: ``['ok', 'success']``

    List of severities that will automatically close the aggregate upon entering the system. This is mainly for icinga/grafana that can close the alert when the status becomes green again




