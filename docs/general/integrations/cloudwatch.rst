.. _integration-cloudwatch:

====================================
Amazon CloudWatch Alarms (input)
====================================

Overview
========

The ``cloudwatch`` plugin is an in-process WebhookReceiver that accepts
Amazon SNS HTTP(S) delivery messages carrying CloudWatch alarm state-change
notifications. When a CloudWatch alarm transitions state, CloudWatch publishes
a message to an SNS topic, and SNS POSTs the message to this endpoint. The
plugin maps the alarm payload to a ``snoozetypes.Record`` and submits it to
the Snooze processing pipeline.

The recommended topology is:

1. A CloudWatch alarm configured to publish to an SNS topic.
2. An SNS subscription of type ``HTTPS`` pointing to this endpoint.
3. SNS delivers one HTTP POST per state transition; the plugin handles
   subscription confirmation automatically.

Configuration
=============

Inbound URL
-----------

The plugin mounts at::

    /api/v1/webhook/cloudwatch

No authentication is required on this endpoint by design (SNS pushes without
credentials). Restrict access at the network level (VPC, security group,
firewall) and/or by TopicArn if needed. SNS message-signature verification is
available as opt-in hardening — see *Notes & limitations* below.

Wiring SNS to this endpoint
-----------------------------

1. **Create (or identify) an SNS topic** that your CloudWatch alarms will
   publish to. Note its ARN, e.g.
   ``arn:aws:sns:us-east-1:123456789012:snooze-alerts``.

2. **Create an HTTPS subscription** on the topic pointing to the snooze
   endpoint:

   .. code-block:: console

       $ aws sns subscribe \
           --topic-arn arn:aws:sns:us-east-1:123456789012:snooze-alerts \
           --protocol https \
           --notification-endpoint https://snooze.example.com/api/v1/webhook/cloudwatch

   SNS will immediately send a ``SubscriptionConfirmation`` POST. The plugin
   GETs the ``SubscribeURL`` included in that message to auto-confirm the
   subscription.

3. **Configure each CloudWatch alarm** to send notifications to that topic
   (for ALARM, OK, and/or INSUFFICIENT_DATA transitions as desired):

   .. code-block:: console

       $ aws cloudwatch put-metric-alarm \
           --alarm-name HighCPU \
           --alarm-description "CPU usage > 90% for 5 minutes" \
           --namespace AWS/EC2 \
           --metric-name CPUUtilization \
           --dimensions Name=InstanceId,Value=i-0abc123def456789 \
           --statistic Average \
           --period 300 \
           --evaluation-periods 1 \
           --threshold 90 \
           --comparison-operator GreaterThanOrEqualToThreshold \
           --alarm-actions arn:aws:sns:us-east-1:123456789012:snooze-alerts \
           --ok-actions    arn:aws:sns:us-east-1:123456789012:snooze-alerts

Testing with curl
-----------------

Post a minimal captured SNS Notification envelope (the ``Message`` value is
an escaped CloudWatch alarm JSON string):

.. code-block:: console

    $ curl -s -X POST https://snooze.example.com/api/v1/webhook/cloudwatch \
        -H "Content-Type: application/json" \
        -H "x-amz-sns-message-type: Notification" \
        -d '{
          "Type": "Notification",
          "MessageId": "test-001",
          "TopicArn": "arn:aws:sns:us-east-1:123456789012:snooze-alerts",
          "Subject": "ALARM: HighCPU entered ALARM state",
          "Message": "{\"AlarmName\":\"HighCPU\",\"AlarmDescription\":\"CPU too high\",\"NewStateValue\":\"ALARM\",\"NewStateReason\":\"Threshold crossed\",\"Region\":\"us-east-1\",\"StateChangeTime\":\"2024-01-15T12:00:00.000+0000\",\"Trigger\":{\"Namespace\":\"AWS/EC2\",\"MetricName\":\"CPUUtilization\",\"Dimensions\":[{\"name\":\"InstanceId\",\"value\":\"i-0abc123def456\"}]}}",
          "Timestamp": "2024-01-15T12:00:01.000Z"
        }'

A successful response looks like::

    {"accepted":1,"received":1,"status":"ok"}

Field reference
---------------

Alarm-to-record mapping:

+-------------------------+---------------------------------------------------+
| Record field            | Source                                            |
+=========================+===================================================+
| ``Source``              | Always ``"cloudwatch"``                           |
+-------------------------+---------------------------------------------------+
| ``Host``                | First Dimension named ``InstanceId``, ``host``,   |
|                         | or ``Host``; fallback: ``AlarmName``              |
+-------------------------+---------------------------------------------------+
| ``Process``             | ``Trigger.MetricName``; fallback:                 |
|                         | ``Trigger.Namespace``                             |
+-------------------------+---------------------------------------------------+
| ``Severity``            | ``ALARM`` → ``critical``;                         |
|                         | ``OK`` → ``info``;                                |
|                         | ``INSUFFICIENT_DATA`` → ``warning``               |
+-------------------------+---------------------------------------------------+
| ``State``               | ``OK`` → ``"close"``; others → ``""``            |
+-------------------------+---------------------------------------------------+
| ``Message``             | ``NewStateReason``; fallback: ``AlarmDescription``|
+-------------------------+---------------------------------------------------+
| ``Raw``                 | ``alarmName``, ``alarmDescription``,              |
|                         | ``newStateValue``, ``newStateReason``,            |
|                         | ``region``, ``stateChangeTime``,                  |
|                         | ``namespace``, ``metricName``,                    |
|                         | ``dimensions``, ``topicArn``                      |
+-------------------------+---------------------------------------------------+

End-to-end test setup
=====================

The e2e test in ``e2e_test.go`` posts a realistic SNS Notification body to a
live snooze-server and asserts an HTTP 2xx response. It is skipped unless the
environment variable ``SNOOZE_E2E_CLOUDWATCH_URL`` is set.

.. list-table::
   :header-rows: 1

   * - Variable
     - Description
   * - ``SNOOZE_E2E_CLOUDWATCH_URL``
     - Full URL of the cloudwatch webhook endpoint on a running snooze-server,
       e.g. ``http://localhost:5200/api/v1/webhook/cloudwatch``

.. code-block:: console

    $ export SNOOZE_E2E_CLOUDWATCH_URL="http://localhost:5200/api/v1/webhook/cloudwatch"
    $ go test -run TestCloudWatchE2E ./internal/pluginimpl/cloudwatch/...

The sample envelope used by the E2E test is **unsigned**, so it exercises the
default (verify-off) path. To E2E-test the signed path, set
``config.ingest.sns_verify: true`` on the running server and POST a message
carrying valid ``SignatureVersion`` / ``Signature`` / ``SigningCertURL``
fields, or drive a real SNS subscription end-to-end.

Notes & limitations
===================

SNS message-signature verification (opt-in)
   By default this plugin does **not** verify the SNS message signature, so its
   behavior is unchanged from earlier releases. Set
   ``config.ingest.sns_verify: true`` to opt in to verification. When enabled,
   every ``SubscriptionConfirmation`` **and** ``Notification`` is verified
   *before* it is processed — and, critically, a ``SubscriptionConfirmation``
   is verified before its ``SubscribeURL`` is auto-confirmed. The plugin:

   #. Rebuilds the AWS SNS canonical *string to sign*. For ``Notification`` the
      keys, in order, are ``Message``, ``MessageId``, ``Subject`` (only when
      present), ``Timestamp``, ``TopicArn``, ``Type``. For
      ``SubscriptionConfirmation`` / ``UnsubscribeConfirmation`` they are
      ``Message``, ``MessageId``, ``SubscribeURL``, ``Timestamp``, ``Token``,
      ``TopicArn``, ``Type``. Each pair is emitted as ``key\nvalue\n``.
   #. base64-decodes ``Signature``.
   #. Validates the ``SigningCertURL`` host against
      ``^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$``. Any other host (or a
      non-``https`` URL) is rejected **without fetching it**, blocking SSRF.
   #. Fetches the certificate, parses the PEM x509 cert, takes its RSA public
      key, and verifies with ``rsa.VerifyPKCS1v15`` using SHA1 when
      ``SignatureVersion`` is ``"1"`` and SHA256 when ``"2"``.

   Any verification failure responds **403** and the message is not processed.
   When ``sns_verify`` is off (the default) no certificate is fetched and no
   verification occurs.

   Network-level controls remain recommended as defense-in-depth: restrict
   access at the network layer (VPC endpoint policies, security groups, or an
   upstream firewall that whitelists SNS IP ranges) and/or validate the
   ``TopicArn`` in a downstream Snooze rule.

SNS IP ranges
   Amazon publishes the current list of SNS delivery IP ranges in the AWS IP
   address ranges JSON file (``"service": "SNS"``). Consider adding these
   ranges to your firewall allowlist.

HTTPS only
   Configure your SNS subscription with protocol ``https`` (not ``http``) so
   that deliveries are encrypted in transit. The snooze-server must present a
   valid TLS certificate, or SNS will fail to deliver.

Subscription auto-confirmation
   The plugin confirms ``SubscriptionConfirmation`` requests automatically by
   GETting the ``SubscribeURL``. No operator action is required beyond
   creating the SNS subscription.

State ``INSUFFICIENT_DATA``
   Records for ``INSUFFICIENT_DATA`` are emitted with severity ``warning`` and
   no ``State`` (i.e. not a resolve). They represent a monitoring gap, not a
   recovery.
