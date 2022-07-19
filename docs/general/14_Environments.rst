.. _environments:

============
Environments
============

.. figure:: images/web_alerts.png
    :align: center
    :target: ../_images/web_alerts.png

Overview
--------

Environments are a web interface oriented feature allowing users to filter alerts based on a custom :ref:`condition <conditions>`.

They are displayed on the very top on the web interface's Alert page.

Multiple environments can be selected.

Web interface
-------------

.. figure:: images/web_environments.png
    :align: center

:Name*: Name of the environment
:|filter|: Condition used to define the environment
:Group: Group number
:Color: Change the button's display
:Comment: Description

.. |filter| replace:: :ref:`Condition <conditions>`

.. note:: Use drag&drop to change the display order.

.. note:: Environments in **same** groups are additive (OR). Environments in **different** groups are multiplicative (AND).
