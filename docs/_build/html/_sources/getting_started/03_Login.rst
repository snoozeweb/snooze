.. _login:

===========================
Log in to the web interface
===========================

.. image:: images/web_login.png
    :align: center

URL
===

By default the web interface listens locally on ``http://localhost:5200``.

Bootstrap ``root`` user (v2.0)
==============================

If ``create_root_user`` in ``/etc/snooze/server-go/core.yaml`` is left
at its default (``true``), a local ``root`` user is created the first
time ``snooze-server`` starts.

Unlike Snooze 1.x — which used the hard-coded password ``root`` — the
2.0 bootstrap generates a 24-byte random password, bcrypt-hashes it,
and prints the plaintext **once** to stderr:

.. code-block:: console

    snooze-server: bootstrap: root password = NaMd…rotateMeNow

Copy that string out of ``journalctl -u snooze-server`` and rotate it as
soon as you're in:

.. code-block:: console

    $ TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' \
        -d '{"username":"root","password":"<bootstrap-password>"}' \
        http://localhost:5200/api/v1/login/local | jq -r .token)
    $ curl -X PATCH -H "Authorization: Bearer $TOKEN" \
        -H 'Content-Type: application/json' \
        -d '{"password":"<your-new-password>"}' \
        http://localhost:5200/api/v1/user/root@local

Rescue path (lost root password)
================================

If the printed password was missed, an operator with shell access on
the server can mint a one-shot rescue JWT over the admin Unix socket:

.. code-block:: console

    $ sudo snooze-server root-token
    eyJhbGciOiJIUzI1NiI…

The socket is at ``/var/run/snooze/admin.sock`` by default and is
guarded by peer-cred: only the process owner (and root) can read from
it. The returned JWT carries the admin role for a 5-minute lease.

JWT vs Bearer
=============

The 1.x ``Authorization: JWT <token>`` scheme is **no longer accepted**.
Use:

.. code-block:: text

    Authorization: Bearer <token>

LDAP Authentication
===================

:ref:`Configure LDAP Authentication <LDAP-configuration>`
