.. _users:

=============================
User creation and permissions
=============================

Only authenticated users with sufficient permissions can interact with Snooze server. Permissions are given to users by assigning them Roles.

Roles
=====

.. image:: images/web_roles.png
    :align: center

:Name*: Name of the role.
:Permissions: List of permissions granted by the role.
:Groups: List of groups provided by the authentication backend. If a user successfully logs in and is a member of a group, this role will get automatically assigned to the user. See :ref:`Static Roles <static_roles>`
:Comment: Description

Permissions
-----------

Explanation of all default permissions:

:rw_all: Read and Write for All. Full privileges on any resource in Snooze server.
:ro_all: Read Only for All. Can view everything but cannot add/edit/delete anything.
:rw_X: Read and Write for X. Full privileges on resource X.
:ro_X: Read Only for X. Can view everything on resource X but cannot add/edit/delete it.
:can_comment: Allow to acknowledge, re-escalate or comment any received alert. :ref:`More on Alerts <alerts>`

Users
=====

.. image:: images/web_users.png
    :align: center

:Username*: Account username.
:Roles: List of roles assigned to the user.
:Password*: When creating a user, this field is required to set up the user's first password. When editing a user, leave it blank to apply no changes to it. Note: for LDAP users, passwords are not displayed.

Static Roles
------------

.. _static_roles:

.. image:: images/web_users_group.png
    :align: center

Roles that automatically assigned to a user because of their group membership coming from the authentication backend will appear as locked.  They cannot be removed unless either the Role's groups are changed or the user's group membership is changed.
