.. _settings:

========
Settings
========

Overview
========

General configuration
---------------------

Located in ``/etc/snooze/server/general.yaml``, these settings can also be set up in the web interface **General** tab:

:metrics_enabled (``true``): Enable or disable :ref:`metrics <metrics>`.
:local_users_enabled (``true``): Enable or disable Local Users Authentication.
:anonymous_enabled (``false``): Enable or disable Anonymous login.
:default_auth_backend (``local``): Default Authentication backend.
:ok_severities (``ok, success``): Space separated severities used to automatically :ref:`close <close>` an alert (case incensitive).

LDAP configuration
------------------

.. _ldap:

LDAP Authentication can be configured in the **LDAP** tab.

Web interface
=============

.. figure:: images/web_settings_general.png
    :align: center
