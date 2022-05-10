.. _installation:

===========================
Installation on RHEL/Debian
===========================

Installation on CentOS/RHEL
===========================

.. code-block:: console

    $ wget https://rpm.snoozeweb.net -O snooze-server-latest.rpm
    $ sudo yum localinstall snooze-server-latest.rpm
    $ sudo systemctl start snooze-server

Installation on Ubuntu/Debian
=============================

.. code-block:: console

    $ wget https://deb.snoozeweb.net -O snooze-server-latest.deb
    $ sudo apt install snooze-server-latest.deb
    $ sudo systemctl start snooze-server

.. important::

    By default, Snooze is using a single file to store its database and therefore can run out of the box without any additional configuration or dependency. While this implementation is convenient for testing purpose, it is heavily recommended to switch the database configuration to MongoDB.

Web interface access
====================

.. code-block:: console

    http://localhost:5200
