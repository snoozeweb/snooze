.. _integration-azuremonitor:

===================================
Azure Monitor (input)
===================================

Overview
========

The ``azuremonitor`` plugin is an in-process WebhookReceiver that accepts
`Azure Monitor Common Alert Schema <https://learn.microsoft.com/en-us/azure/azure-monitor/alerts/alerts-common-schema>`_
payloads pushed by an Azure Monitor Action Group. It maps the incoming alert
to a ``snoozetypes.Record`` and submits it to the Snooze processing pipeline.

This is a **push** integration: Azure calls Snooze, not the other way around.
No credentials are required on the Azure side (the endpoint is unauthenticated
by design, matching the policy of the Grafana and Alertmanager receivers).

Configuration
=============

The plugin registers itself automatically when the Snooze server loads. No
additional server-side configuration is required. The inbound webhook URL is:

.. code-block:: text

    /api/v1/webhook/azuremonitor

Full external example (replace the host)::

    https://snooze.example.com/api/v1/webhook/azuremonitor

Wiring an Azure Monitor Action Group
-------------------------------------

1. In the Azure Portal, open **Monitor → Alerts → Action groups**.
2. Create or edit an Action Group.
3. Under **Actions**, add an action of type **Webhook**.
4. Set the URI to your Snooze webhook URL.
5. **Enable the common alert schema** (the toggle in the Webhook action
   form). This is the format this plugin understands.
6. Assign the Action Group to one or more alert rules.

When the alert fires (``monitorCondition == "Fired"``) Azure POSTs a Common
Alert Schema body to Snooze; when it resolves (``monitorCondition ==
"Resolved"``) a second POST is made and Snooze emits a record with
``State: "close"`` to resolve the matching open alert.

curl example
------------

The following command simulates an Azure Monitor "Fired" notification:

.. code-block:: console

    $ curl -X POST https://snooze.example.com/api/v1/webhook/azuremonitor \
        -H 'Content-Type: application/json' \
        -d '{
          "schemaId": "azureMonitorCommonAlertSchema",
          "data": {
            "essentials": {
              "alertId": "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.AlertsManagement/alerts/my-alert",
              "alertRule": "High CPU on web-1",
              "severity": "Sev1",
              "signalType": "Metric",
              "monitorCondition": "Fired",
              "monitoringService": "Platform",
              "alertTargetIDs": [
                "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/web-1"
              ],
              "firedDateTime": "2026-05-27T10:00:00Z",
              "description": "CPU exceeded 90% for 5 minutes"
            },
            "alertContext": {
              "conditionType": "SingleResourceMultipleMetricCriteria",
              "properties": {"threshold": "90"}
            }
          }
        }'

Expected response:

.. code-block:: json

    {"accepted": 1, "received": 1, "status": "ok"}

Field reference
---------------

The following table describes how Azure Monitor fields map to Snooze record
fields.

.. list-table::
   :header-rows: 1
   :widths: 25 25 50

   * - Snooze field
     - Source
     - Notes
   * - ``Source``
     - (fixed)
     - Always ``"azuremonitor"``
   * - ``Host``
     - ``data.essentials.alertTargetIDs[0]``
     - Last ``/``-separated segment of the resource ID (e.g.
       ``"web-1"``). Falls back to ``alertRule`` when the array is empty.
   * - ``Process``
     - ``data.essentials.monitoringService`` / ``signalType``
     - Concatenated as ``monitoringService/signalType`` (e.g.
       ``"Platform/Metric"``).
   * - ``Severity``
     - ``data.essentials.severity``
     - ``Sev0``/``Sev1`` → ``critical``; ``Sev2`` → ``error``;
       ``Sev3`` → ``warning``; ``Sev4`` → ``info``.
       Resolved alerts are always downgraded to ``info``.
   * - ``Message``
     - ``data.essentials.description``
     - Falls back to ``alertRule`` when description is empty.
   * - ``State``
     - ``data.essentials.monitorCondition``
     - ``"close"`` when ``monitorCondition == "Resolved"``; empty otherwise.
   * - ``Raw``
     - ``data.essentials`` + ``data.alertContext``
     - All essentials fields plus the compact alert context are stored
       in ``Raw`` so downstream rules can match on them.

Severity mapping
~~~~~~~~~~~~~~~~

.. code-block:: text

    Sev0, Sev1  →  critical
    Sev2        →  error
    Sev3        →  warning
    Sev4        →  info
    (unknown)   →  critical  (firing) / info  (resolved)

Non-Common-Alert-Schema fallback
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

If the payload lacks ``data.essentials`` (i.e. it is not a Common Alert
Schema body), the plugin attempts a best-effort parse using any top-level
``title`` and ``description`` fields and defaults to severity ``critical``.

End-to-end test setup
=====================

The e2e test posts a realistic Common Alert Schema payload to a live
snooze-server instance and asserts a 2xx response.

Required environment variable:

``SNOOZE_E2E_AZUREMONITOR_URL``
    Full URL to the Azure Monitor webhook endpoint on the target snooze-server.

.. code-block:: console

    $ export SNOOZE_E2E_AZUREMONITOR_URL=https://snooze.example.com/api/v1/webhook/azuremonitor
    $ go test -run TestAzureMonitorE2E ./internal/pluginimpl/azuremonitor/...

Notes & limitations
===================

- **OTLP / Azure Monitor Workspace** metrics are not supported by this plugin;
  it only handles Action Group webhook notifications using the Common Alert
  Schema.
- **Signature verification** is not implemented. Azure Monitor does not sign
  webhook payloads. If you need request authentication, place a reverse proxy
  (e.g. nginx with ``auth_request``) in front of Snooze.
- **Multiple target resources**: when an alert targets more than one resource
  (``alertTargetIDs`` has multiple entries) only the first entry is used for
  the ``Host`` field.
- **Activity log / Service Health alerts** use the same Common Alert Schema
  envelope and are handled correctly; the ``alertContext`` fields are placed
  verbatim in ``Raw`` for downstream rule matching.
