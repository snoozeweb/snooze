.. _rules:

=====
Rules
=====

.. figure:: images/architecture.png
    :align: center

    Architecture - Rules plugin

Overview
========

Add, modify or delete fields from a alert.

Alerts have to match a Rule's conditions in order to being processed.

Rules are very useful to analyze incoming alerts and add infos that were not in the original log.

.. code-block:: yaml
    :caption: Example

    # Alert before being processed by Rule
    host: prod-syslog01.example.com

.. code-block:: yaml

    # Rule
    name: is_production
    condition: host MATCHES ^prod.*
    modification: SET environment = production

.. code-block:: yaml

    # Alert after being processed by Rule
    host: prod-syslog01.example.com
    rules: ['is_production']
    environment: production

Any alert matching a Rule will have a new field ``rules`` added with the list of matched Rules.

Rules resolution order is important. It allows Rules to create fields that can be used in subsequent Rules.

Rules can have an optional field called ``parents`` which can hold a list of Rule UIDs. These Rules will be processed only if all parents conditions have been met in the first place.

This design allowing Rules to be nested is very convenient to avoid repeating the same conditions across multiple Rules.

Jinja templates
===============

.. _jinja:

It is possible to use Jinja templates to render the alert's fields for any modification

*Example*: ``["SET", "environment", "{{ env }}"]`` will create a new field **environment** using the content of the field  **env** from the same alert.

More info on `Jinja website <https://jinja.palletsprojects.com/en/2.11.x/templates/>`_.

Web interface
=============

.. image:: images/web_rules.png
    :align: center

:Name*: Name of the rule.
:|condition|: This rule will be triggered only if this condition is met. Leave it blank to always match.
:|modification|: List of changes to apply to the alert.
:Comment: Description.

.. |condition| replace:: :ref:`Condition <conditions>`
.. |modification| replace:: :ref:`Modification <modifications>`

**Note**: Use drag&drop to change the rule's order and/or use nesting:

.. image:: images/web_rules_children.png
    :align: center


Modifications
=============

.. _modifications:

All modifications are applied in order. They provide a lot of control over incoming alerts.

List of modifications:

* `Set`_
* `Delete`_
* `Append (to array)`_
* `Delete (from array)`_
* `Regex parse (capture)`_
* `Regex sub`_
* `Key-value mapping`_

Set
---

Modify a field or create it if it does not exists.

.. code-block:: json
    :caption: Source alert

    {
        "hostname": "prod-unit01"
    }

============ =========== ==========
modification field       value
============ =========== ==========
SET          environment production
============ =========== ==========

.. code-block:: json
    :caption: Alert after modification
    :emphasize-lines: 3

    {
        "hostname": "prod-unit01",
        "environment": "production"
    }

This modification added a field ``environment`` to the alert.

Delete
------

Delete a field.

.. code-block:: json
    :caption: Source alert

    {
        "name": "john",
        "password": "Ch@ng3m3!"
    }

============ ========
modification field
============ ========
DELETE       password
============ ========

.. code-block:: json
    :caption: Alert after modification

    {
        "name": "john",
    }

This modification deleted the sensitive field ``password`` from the alert.

Append (to array)
-----------------

Append an element to a array typed field.

.. code-block:: json
    :caption: Source alert

    {
        "permissions": ["read"]
    }

============ =========== =====
modification field       value
============ =========== =====
ARRAY_APPEND permissions write
============ =========== =====

.. code-block:: json
    :caption: Alert after modification

    {
        "permissions": ["read", "write"]
    }

This modification added ``write`` to the list of ``permissions``.

Delete (from array)
-------------------

Delete the first matching element from an array typed field if it exists.

.. code-block:: json
    :caption: Source alert

    {
        "names": ["stanley", "john", "erika"]
    }

============ ===== =====
modification field value
============ ===== =====
ARRAY_DELETE names john
============ ===== =====

.. code-block:: json
    :caption: Alert after modification

    {
        "names": ["stanley", "erika"]
    }

This modification removed ``john`` from the list of ``names``.

Regex parse (capture)
---------------------

Parse a field with a regex. The named capture groups will be merged with the alert.

See `python regex syntax <https://docs.python.org/3/library/re.html#regular-expression-syntax>`_ for more details on the regex named capture group syntax.

.. code-block:: json
    :caption: Source alert

    {
        "message": "CRON[12345]: my error message"
    }

============ ======= =========================
modification field   regex with capture groups
============ ======= =========================
REGEX_PARSE  message .. code-block:: xml

                       (?P<appname>.*?)\[(?P<pid>)\d+\]: (?P<message>.*)
============ ======= =========================

.. code-block:: json
    :caption: Alert after modification

    {
        "appname": "CRON",
        "pid": "12345",
        "message": "my error message"
    }

This modification splitted the field ``message`` into 3 more relevant fields ``appname``, ``pid`` and ``message`` (overwritten).

Regex sub
---------

Parse a field with a regex. Substitute the matching pattern then output a new field.

.. code-block:: json
    :caption: Source alert

    {
        "message": "Wrong password: Ch@ngem3"
    }

============ ======= ======= =================== ====================
modification field   output  regex parse         sub
============ ======= ======= =================== ====================
REGEX_SUB    message message .. code-block:: xml  .. code-block:: xml

                                (password:) (.*)    \g<1> ***
============ ======= ======= =================== ====================

.. code-block:: json
    :caption: Alert after modification

    {
        "message": "Wrong password: ***"
    }

This modification splitted the field ``message`` into 3 more relevant fields ``appname``, ``pid`` and ``message`` (overwritten).

Key-value mapping
-----------------

.. _kv_mapping:

Try to match a field with a key present in a KV. If found, output the value in a new field.

.. caution::
    a Key-value store has to be created beforehand. See :ref:`Key-values <kv>`.

============ ======= =====
dictionnary  key     value
============ ======= =====
roles        john    admin
roles        stanley user
============ ======= =====

.. code-block:: json
    :caption: Source alert

    {
        "name": "john"
    }

============ ===== ===== ======
modification dict  field output
============ ===== ===== ======
KV_SET       roles name  role
============ ===== ===== ======

.. code-block:: json
    :caption: Alert after modification
    :emphasize-lines: 3

    {
        "name": "john"
        "role": "admin"
    }

This modification matched ``john`` in the Key-value ``roles``, therefore adding the field ``role`` with its corresponding value ``admin``.
