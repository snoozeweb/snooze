.. _integration-patlite:

======================
Patlite (output)
======================

Overview
========

The **patlite** plugin is an in-process Notifier that controls the
light and alarm state of a
`Patlite <https://www.patlite.com/>`_ tower-light device (LR/LE/NH
series) over HTTP. It is wired as a notification *Action* in the Snooze
UI: when an alert matches a Notification rule that references a patlite
action, the plugin issues an HTTP GET request to the device's control
endpoint, setting the light colour and mode based on the alert's
severity.

The plugin targets the HTTP control API exposed by modern Patlite
firmware (NH-series and equivalent). The Python 1.x plugin spoke the
legacy TCP socket protocol on port 10000; this Go rewrite uses the HTTP
surface instead.

**How the severity mapping works:**

1. The record's ``severity`` field (case-insensitive) is looked up in
   ``severity_map``.
2. If not found, the special key ``default`` is used.
3. If neither is found, the device is sent a ``?clear=1`` pulse (all
   lights off) so the device is never left in a stale state.

When the matched entry has ``color: clear`` (or an empty/``off`` colour)
the query is ``?clear=1``; otherwise it is
``?color=<color>&state=<state>``.

Configuration
=============

Wire the plugin through a **Notification → Action** in the Snooze UI or
configuration file. Set the action type to ``patlite`` and fill the
``action_form`` fields described below.

Action fields
-------------

.. list-table::
   :widths: 20 14 18 48
   :header-rows: 1

   * - Field
     - Component
     - Default
     - Description
   * - ``host``
     - String
     - *(required)*
     - Hostname or IP address of the Patlite device. May include the
       scheme (``http://`` or ``https://``); plain hostnames default to
       ``http``.
   * - ``port``
     - Number
     - ``80``
     - HTTP port of the Patlite device. Ignored when the host already
       contains a port.
   * - ``path``
     - String
     - ``/api/control``
     - HTTP control endpoint path on the device. Adjust for firmware
       variants that use a different path (e.g.
       ``/cgi-bin/lamp.cgi`` on LE-A1 firmware).
   * - ``timeout``
     - Number
     - ``5``
     - HTTP request timeout in seconds. Patlite devices on a healthy
       LAN typically respond in tens of milliseconds.
   * - ``tls_insecure``
     - Boolean
     - ``false``
     - Skip TLS certificate verification when the path uses
       ``https://``. Use only for trusted devices with self-signed
       certificates.
   * - ``severity_map``
     - Object
     - *(see below)*
     - Map of record severity to ``{color, state}`` pairs. The special
       key ``default`` applies when the record's severity is not found
       in the map. Omit to use the built-in default mapping.

Severity map schema
-------------------

Each value in ``severity_map`` is an object with two keys:

- ``color`` — one of ``red``, ``amber``, ``green``, ``blue``,
  ``white``, ``clear``, or ``off``. Use ``clear`` (or ``off``, or an
  empty string) to turn all lights off.
- ``state`` — one of ``on``, ``off``, ``blink1``, ``blink2``. Ignored
  when ``color`` is ``clear``/``off``. Defaults to ``on`` when omitted.

A bare string value (e.g. ``"red"``) is also accepted as shorthand for
``{color: red, state: on}``.

Default severity map
--------------------

When ``severity_map`` is not configured the following built-in mapping
applies:

.. list-table::
   :widths: 25 20 20 35
   :header-rows: 1

   * - Severity
     - Color
     - State
     - Notes
   * - ``critical``
     - ``red``
     - ``on``
     -
   * - ``error``
     - ``red``
     - ``on``
     -
   * - ``warning``
     - ``amber``
     - ``on``
     -
   * - ``info``
     - ``green``
     - ``on``
     -
   * - ``default``
     - ``clear``
     - *(N/A)*
     - Applies to any severity not listed above; clears all lights.

Example
=======

.. code-block:: yaml
   :caption: Minimal configuration (default severity map)

   host:    "192.168.1.200"
   port:    80
   timeout: 5

.. code-block:: yaml
   :caption: Custom severity map with blinking lights for critical

   host:    "patlite-rack1.lan"
   port:    80
   path:    "/api/control"
   timeout: 5
   severity_map:
     critical: {color: red,   state: blink2}
     error:    {color: red,   state: on}
     warning:  {color: amber, state: blink1}
     info:     {color: green, state: on}
     notice:   {color: green, state: on}
     default:  {color: clear}

.. code-block:: yaml
   :caption: HTTPS endpoint with self-signed certificate

   host:        "https://patlite-secure.lan"
   port:        443
   tls_insecure: true
   timeout:     10

Testing / verifying
===================

1. **Create a test action** in the Snooze UI (Actions → New → patlite)
   with ``host`` pointing at the device and a ``severity_map`` covering
   the severities you want to test.

2. **Create a matching Notification rule** that routes a specific
   condition (e.g. ``source = "patlite-test"``) to the new action.

3. **Send a test alert for each severity** via the CLI and observe the
   device::

      $ snooze alert source=patlite-test severity=critical \
          "message=patlite critical test"
      $ snooze alert source=patlite-test severity=warning \
          "message=patlite warning test"
      $ snooze alert source=patlite-test severity=info \
          "message=patlite info test"

4. **Verify the device** changes colour and mode as expected. Check the
   snooze-server logs for any HTTP errors from the device if the light
   does not respond.

To test the clear path, send an alert with a severity not present in the
map (or with ``state: close``) and confirm that all lights turn off.

You can also test the control URL directly with curl to confirm the
device firmware accepts the expected query shape before wiring it through
Snooze::

   # Turn on red
   $ curl "http://192.168.1.200/api/control?color=red&state=on"
   # Clear all lights
   $ curl "http://192.168.1.200/api/control?clear=1"

Notes & limitations
===================

- The plugin targets the HTTP control API introduced in NH-series
  firmware. Older devices using the TCP socket protocol on port 10000
  (as used by the Python 1.x plugin) are not supported.
- The exact query-string shape (``?color=…&state=…`` or ``?clear=1``)
  matches the most common NH-series HTTP firmware variant. For LE-A1 or
  other models with a different API, adjust ``path`` accordingly. If the
  query format itself differs, a custom webhook action is a better fit.
- Only one HTTP GET is issued per ``Send`` call. The plugin does not poll
  for confirmation; a successful ``2xx`` response is treated as
  acknowledgement of the command.
- HTTP status codes ``400`` and above are treated as errors and surfaced
  in the snooze-server logs.
- Patlite batching is not supported (there is no batch knob in
  ``metadata.yaml``). Each matching alert sends its own control request.
- The ``host`` field can include the scheme (``http://`` or ``https://``)
  for firmware that exposes HTTPS. When no scheme is present, ``http``
  is used.
