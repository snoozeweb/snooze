.. _housekeeping:

============
Housekeeping
============

Overview
--------

The housekeeper is a subprocess of Snooze server meant to automatically cleanup data that is not neeeded anymore,
preventing Snooze server to grow indefinitely large.

Configuration
-------------

Located in ``/etc/snooze/server/housekeeper.yaml``, these settings can also be set up using the web interface on the **Settings** page:

:trigger_on_startup (``true``): Trigger the housekeeper on Startup
:record_ttl (``172800``): Assign a time to live for any new alert. -1 for no expiration.  Default is every 2 days.

    .. caution::

        It is not recommended to have alerts that do not expire. As they will keep on filling up disk space, their growing number will also decrease overall performances.
:cleanup_alert (``300``): Execute a cleanup job every X seconds. <=0 for no cleanup. Default is every 5min.
:cleanup_comment (``86400``): Execute a cleanup job every X seconds. <=0 for no cleanup. Default is daily.
:cleanup_snooze (``259200``): Cleanup expired snooze filters that are X seconds old (job executed once at 00:00AM). <=0 for no cleanup. Default is every 3 days.
:cleanup_notification (``259200``): 'Cleanup expired notifications that are X seconds old (job executed once at 00:00AM). <=0 for no cleanup'. Default is every 3 days.
:cleanup_audit (``2419200``): Cleanup expired audit logs that are X seconds old (job executed once at 00:00AM). <=0 for no cleanup. Default is every 28 days.

Web interface
-------------

.. figure:: images/web_housekeeping.png
    :align: center
