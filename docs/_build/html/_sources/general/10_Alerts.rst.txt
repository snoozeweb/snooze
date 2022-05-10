.. _alerts:

=============
Manage alerts
=============

.. figure:: images/web_alerts.png
    :align: center
    :target: ../_images/web_alerts.png

Overview
========

This page will list all the tools available to manage alerts.

Alerts that have not been :ref:`snoozed <snooze_filters>`, :ref:`acknowledged <acknowledge>` or :ref:`closed <close>` will be displayed under the first tab of the **Alerts** page on the web interface.

Alerts that have been snoozed will be displayed under the **Snoozed** tab on the same page.

Alerts that have been :ref:`re-escalated <reescalate>` or :ref:`re-opened <reopen>`  will be displayed under the **Re-escalated** tab on the same page.

Alerts that have been :ref:`closed <close>` will be displayed under the **Closed** tab on the same page.

Alerts that have been :ref:`shelved <shelve>` will be displayed under the **Shelved** tab on the same page.

Alert states
============

User interaction allows an alert to switch between states. Here are the different states an alert can have:

:``-``: A new alert will always have no initial state, meaning nobody has interacted with it yet.
:``ack``: :ref:`Acknowledged <acknowledge>`.
:``esc``: :ref:`Re-escalated <reescalate>`.
:``close``: :ref:`Closed <close>`.
:``open``: :ref:`Re-opened <reopen>`.

Acknowledge
-----------

.. _acknowledge:

Used to let people know that someone is taking care of the issue related to the alert.

Acknowledged alerts will stop getting :ref:`notified <frequency>`.

Re-escalate
-----------

.. _reescalate:

After being acknowledged, an alert can get re-escalated.

It can be done automatically by an :ref:`aggregate rule <aggregate_rules>` after the throttle period ended or a field from the watchlist got updated.

It can be done manually by the user to have the alert go through the full processing once more, meaning it can get notified again or snoozed.
:ref:`Modifications <modifications>` can be applied to the alert beforehand.

Close
-----

.. _close:

Used to let people know that the issue related to the alert is resolved.

Alerts can get closed automatically if their **severity** field is in the list of defined **OK Severities** in :ref:`Settings <settings>`

Closed alerts will stop getting :ref:`notified <frequency>`.

Re-open
-------

.. _reopen:

After being closed, an alert can get re-opened.

It can be done automatically by an :ref:`aggregate rule <aggregate_rules>` if the same alert is observed regardless of the throttle period.

It can be done manually by the user to have the alert go through the full processing once more, meaning it can get notified again or snoozed.
:ref:`Modifications <modifications>` can be applied to the alert beforehand.


Alerts TTL
==========

Alerts are automatically cleaned up by the :ref:`housekeeper <housekeeping>` after a certain period of time called **TTL** (Time To Live)

Default TTL is 172800 seconds (2 days). Check the housekeeper page for more information.

Shelve
------

.. _shelve:

A mean to keep some alerts from being deleted is to shelve them. The operation actually deletes their **TTL** field.

Timeline
========

.. figure:: images/web_alerts_ack.png
    :align: center

By clicking on the grey arrow on an alert, a timeline appears. It contains a history of all events and user interactions related to the alert.
There is a possibility to leave a comment as well. An admin can edit or delete any event. By deleting a state event (for example an acknowledgement), the alert goes back to its previous state.
