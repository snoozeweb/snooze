.. _integration-pagerduty:

==================================
PagerDuty (output)
==================================

Overview
========

The PagerDuty integration is an **output** (Notifier) plugin that forwards
Snooze alerts to PagerDuty using the `Events API v2`_. It runs in-process as
part of ``snooze-server``; no additional daemon is required.

For each matching record the plugin posts a ``trigger`` or ``resolve`` event to
``POST {api_base}/v2/enqueue``. A ``resolve`` is sent when ``record.State``
equals ``"close"``; every other state sends a ``trigger``. The ``dedup_key``
field is derived from the record's ``hash`` (set by the Aggregate Rule plugin)
so a later resolve correlates correctly to the original trigger in PagerDuty.

.. _Events API v2: https://developer.pagerduty.com/docs/events-api-v2/overview/


Severity mapping
----------------

Snooze uses syslog-style severity words; PagerDuty accepts exactly four values
(``critical``, ``error``, ``warning``, ``info``). The mapping is:

+------------------------------+----------------+
| Snooze severity              | PagerDuty      |
+==============================+================+
| emergency, critical          | critical       |
+------------------------------+----------------+
| error, err                   | error          |
+------------------------------+----------------+
| warning                      | warning        |
+------------------------------+----------------+
| notice, info, debug          | info           |
+------------------------------+----------------+
| (unknown / empty) + trigger  | critical       |
+------------------------------+----------------+
| (unknown / empty) + resolve  | info           |
+------------------------------+----------------+

Set **Severity** to ``auto`` (the default) to derive the value automatically.
Set it to an explicit value to override the mapping for all alerts routed
through a particular action.


Configuration
=============

Wire the integration through a Snooze notification action. The action_form
fields are:

+----------------+-----------+----------------------------------------------------+
| Field          | Required  | Description                                        |
+================+===========+====================================================+
| Routing Key    | yes       | PagerDuty integration key (Events API v2).         |
|                |           | Stored as a ``Password`` field; never shown after  |
|                |           | saving.                                            |
+----------------+-----------+----------------------------------------------------+
| Severity       | no        | ``auto`` (default) or one of                       |
|                |           | ``critical`` / ``error`` / ``warning`` / ``info``. |
+----------------+-----------+----------------------------------------------------+
| Client         | no        | Label shown in PagerDuty for the originating app.  |
|                |           | Default: ``Snooze``.                               |
+----------------+-----------+----------------------------------------------------+
| Client URL     | no        | URL of the Snooze instance (shown in PagerDuty).   |
+----------------+-----------+----------------------------------------------------+
| API Base URL   | no        | Override for private service regions.              |
|                |           | Default: ``https://events.pagerduty.com``.         |
+----------------+-----------+----------------------------------------------------+
| Timeout        | no        | HTTP request timeout as a Go duration.             |
|                |           | Default: ``10s``.                                  |
+----------------+-----------+----------------------------------------------------+

Field reference
---------------

.. code-block:: yaml
    :caption: Example action_form values (shown as raw YAML for reference)

    routing_key: "r0k3y0000000000000000000000000001"   # required
    severity: auto           # auto | critical | error | warning | info
    client: Snooze
    client_url: https://snooze.example.com
    api_base: https://events.pagerduty.com
    timeout: 10s


The plugin constructs a ``payload.summary`` of the form:

.. code-block:: text

    <severity> on <host>: <message>

truncated to 1 024 Unicode code points (the PagerDuty limit).

``payload.custom_details`` carries a compact map of record metadata (UID,
source, process, environment, hash, tags, raw fields) for use in PagerDuty
event rules or responder notes.


End-to-end test setup
=====================

The package ships an env-gated e2e test in ``e2e_test.go``. When the env var
is absent, the test is skipped automatically so ``go test ./...`` stays green
in CI.

**Steps:**

1. In your PagerDuty account, open (or create) a service.
2. Add a new integration: choose **Events API v2** and copy the
   **Integration Key** (also labelled *Routing Key* in some UIs).
3. Export the key and run the test:

.. code-block:: console

    $ export SNOOZE_E2E_PAGERDUTY_ROUTING_KEY="<your-routing-key>"
    $ go test -v -run TestPagerDutyE2E ./internal/pluginimpl/pagerduty/...

The test triggers a warning-severity event, waits 2 seconds, then resolves it
using the same ``dedup_key`` (``snooze-e2e-test-dedupkey``). Check your
PagerDuty service's activity log to confirm the incident was opened and
immediately resolved.

Environment variables read by the e2e test:

+------------------------------------------+-------------------------------------------+
| Variable                                 | Purpose                                   |
+==========================================+===========================================+
| ``SNOOZE_E2E_PAGERDUTY_ROUTING_KEY``     | Events API v2 integration/routing key.    |
|                                          | **Required** — test is skipped when unset.|
+------------------------------------------+-------------------------------------------+


Notes & limitations
===================

- **HTTP/HTTPS only.** The plugin uses ``net/http`` with the system TLS trust
  store. Self-signed PagerDuty endpoints (on-premises PagerDuty) are not
  supported without a custom ``api_base`` pointing to a trusted endpoint.
- **No gRPC.** The PagerDuty Events API v2 is HTTP/JSON only, so no gRPC
  variant is needed.
- **Rate limits.** PagerDuty imposes an inbound rate limit per routing key.
  Use Snooze's Aggregate Rule plugin to deduplicate noisy alerts before they
  reach this notifier.
- **``links`` field.** The ``links`` array defined by the Events API v2 spec
  is not exposed as an action_form knob today. Add a custom webhook action
  chained before the PagerDuty action if you need to attach URLs.
