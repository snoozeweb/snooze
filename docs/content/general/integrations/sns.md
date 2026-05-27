---
sidebar_position: 37
---

# Amazon SNS (output)

## Overview

The Amazon SNS integration is an **output** (Notifier) plugin that publishes Snooze alerts to an [Amazon Simple Notification Service](https://docs.aws.amazon.com/sns/latest/api/API_Publish.html) topic. It runs in-process as part of `snooze-server`; no additional daemon is required.

For each matching record the plugin issues a single `Publish` call against the SNS Query API. The request is an `application/x-www-form-urlencoded` `POST` to the regional SNS endpoint (`https://sns.<region>.amazonaws.com/` by default), carrying `Action=Publish`, `Version=2010-03-31`, `TopicArn`, `Message` and an optional `Subject`. The request is signed with **AWS Signature Version 4** for service `sns` using the credentials in the action form. An HTTP `200` with a `<PublishResponse>` body is treated as success; on a non-2xx response the SNS error XML (`<Error><Code>…</Code><Message>…</Message></Error>`) is parsed and surfaced in the returned error.

Because SNS is a fan-out service, the actual delivery (email, SMS, HTTPS, Lambda, SQS, …) is governed by the topic's subscriptions, not by Snooze.

## Configuration

Wire a notification to an SNS **action** in the UI (or runtime config) and fill the action form. The credentials live in the action form rather than the host environment so each action can target a distinct topic, region, or IAM principal.

`region` (string, required)  
AWS region of the SNS topic, e.g. `eu-west-1`. Also selects the default endpoint and is part of the SigV4 credential scope.

`topic_arn` (string, required)  
Full ARN of the destination topic, `arn:aws:sns:<region>:<account-id>:<topic-name>`.

`access_key_id` (string, required)  
AWS access key ID of an IAM principal allowed to call `sns:Publish` on the topic.

`secret_access_key` (password, required)  
Secret access key paired with `access_key_id`.

`session_token` (password, optional)  
STS session token for temporary credentials. When set it is sent as `X-Amz-Security-Token` and folded into the SigV4 signature.

`subject` (string, optional, Go `text/template` over the record)  
SNS message subject. Defaults to `{{ .Severity }} on {{ .Host }}`. SNS limits subjects to 100 ASCII characters; the plugin clamps to that length. An empty rendered subject is omitted (SNS allows a message with no subject).

`message` (text, optional, Go `text/template` over the record)  
SNS message body. Defaults to `{{ .Message }}`.

`endpoint` (string, optional)  
Override the SNS endpoint base URL — useful for LocalStack (`http://localhost:4566`) or a VPC endpoint. Defaults to `https://sns.<region>.amazonaws.com/`.

`timeout` (string, optional)  
Request timeout as a Go duration. Defaults to `10s`.

The templates address the record fields directly (the record is the template dot), e.g. `{{ .Host }}`, `{{ .Severity }}`, `{{ .Message }}`, `{{ .Source }}`, `{{ .Environment }}`.

### Field reference

``` yaml
region: eu-west-1
topic_arn: arn:aws:sns:eu-west-1:123456789012:snooze-alerts
access_key_id: AKIAIOSFODNN7EXAMPLE
secret_access_key: wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY
subject: "{{ .Severity }} on {{ .Host }}"
message: "{{ .Host }} ({{ .Source }}): {{ .Message }}"
timeout: 10s
```

## End-to-end test setup

The package ships an env-gated end-to-end test (`internal/pluginimpl/sns/e2e_test.go`) that publishes one real message to a real topic. It skips unless all four required variables are set.

1.  Create an SNS topic and note its ARN:

    ``` console
    $ aws sns create-topic --name snooze-e2e --region eu-west-1
    {
        "TopicArn": "arn:aws:sns:eu-west-1:123456789012:snooze-e2e"
    }
    ```

    Optionally subscribe an email so you can see the message arrive:

    ``` console
    $ aws sns subscribe --region eu-west-1 \
        --topic-arn arn:aws:sns:eu-west-1:123456789012:snooze-e2e \
        --protocol email --notification-endpoint you@example.com
    # then confirm the subscription from the email you receive
    ```

2.  Create an IAM user with permission to publish to that topic, and an access key for it. A minimal policy:

    ``` json
    {
      "Version": "2012-10-17",
      "Statement": [
        {
          "Effect": "Allow",
          "Action": "sns:Publish",
          "Resource": "arn:aws:sns:eu-west-1:123456789012:snooze-e2e"
        }
      ]
    }
    ```

    ``` console
    $ aws iam create-access-key --user-name snooze-e2e
    ```

3.  Export the variables and run the test. Required:

    - `SNOOZE_E2E_SNS_REGION`
    - `SNOOZE_E2E_SNS_TOPIC_ARN`
    - `SNOOZE_E2E_SNS_ACCESS_KEY_ID`
    - `SNOOZE_E2E_SNS_SECRET_ACCESS_KEY`

    Optional:

    - `SNOOZE_E2E_SNS_SESSION_TOKEN` — STS session token for temporary creds.
    - `SNOOZE_E2E_SNS_ENDPOINT` — endpoint override (e.g. LocalStack).

    ``` console
    $ export SNOOZE_E2E_SNS_REGION="eu-west-1"
    $ export SNOOZE_E2E_SNS_TOPIC_ARN="arn:aws:sns:eu-west-1:123456789012:snooze-e2e"
    $ export SNOOZE_E2E_SNS_ACCESS_KEY_ID="AKIA..."
    $ export SNOOZE_E2E_SNS_SECRET_ACCESS_KEY="..."
    $ go test -run E2E ./internal/pluginimpl/sns/...
    ```

## Notes & limitations

- **SigV4 is hand-rolled.** The plugin implements AWS Signature Version 4 on the standard library only (`crypto/hmac`, `crypto/sha256`, `encoding/hex`); there is **no AWS SDK** dependency. The signer is validated in tests against the AWS "Signature Version 4 Test Suite" `get-vanilla` vector.
- **Only the \`\`Publish\`\` action is supported.** Topic management (create/subscribe/etc.) is out of scope — provision the topic and subscriptions with the AWS CLI or console.
- There is no trigger/resolve lifecycle: every matching record (including a `close`) produces one `Publish`. SNS has no notion of resolving a prior message.
- The default endpoint targets the public regional endpoint. Use `endpoint` for FIPS, VPC, dualstack, or LocalStack endpoints.
- No severity remapping is performed; the raw Snooze severity is available to the subject/message templates.

