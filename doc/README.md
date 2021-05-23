# Simplest concept

Receive logs. Aggregate. Send notifications.

The aggregate part means:
* Keep records: count hits, parse fields, remember times
* Document it: let users interact with the data

## Receive

Basic: curl (from any application). Should support basic auth user/password and TLS certs.
More: rsyslog=>curl agent

THe received objects are *records*

## Process

### Pre-Process

Take a *record* and add custom fields and tags based on a *matching_rule*
Output is a *record*

### Aggregate

The core of the application. Should be as flexible as possible to allow different
use cases.
Logs should classify themselves into *aggregations* (need better vocabulary).
Aggregations should be collections of *records* that are parsed.
Aggregations objects should be open and fully customizable by users (adding fields to fit use-case).

How it works:
* Each aggregation have a *matching_rule*, records goes in the aggregation
if the *matching_rule* match
* Aggregations can have a *parsing_rule*, which will parse elements of the *record* into custom fields.

Same as notifications, basic aggregations can be defined in `aggregations/my_aggregation`, but
more complex ones can be defined in `aggregations/my_group/my_aggregation` if the *record* match
the `my_group` *matching_rule* and the `my_aggregation` *matching_rule*. Both *parsing_rule*
will be applied in order, to get as much fields as possible. Fields can override.

A *notification* is a short lived object attached to an *aggregation*. Its main purpose is to give more options to the send notifications component.

An *aggregation* can have additional custom tags. You can create a new *aggregation* by combining tags.

## Send notifications

Notifications should be in paths.
Defaults are like `/notifications/my_notification`, but people
should have the ability to go deeper `/notification/my_subgroup/my_notification`

Notification object should contain:
* Condition on the *record* object. Any custom parameter allowed. The record object
is a json, so anything goes.
* Method (mail), and parameters (address)

Notifications should be scripts. Let's pipe the json inside the said script,
so anything goes.

If an aggregation is piped into it, it will process its *notification* objects and act accordingly.

# Authorization

All operation GET/POST for *receivers*, *aggregations* and *notifications* should
have full authorization control, over every litle field.

Example:
* user U can GET *aggregations* fields F1, F2-*, F3.*, and F4.
* group G can POST *aggregation* A fields F1 and F2

> Note 1: sub-fields are indicated with `.`

> Note 2: It's not possible to indicate fields as `aggregations/A1/F1`, since
> we want to have parent directories for aggregations. So we will have to keep
> them in the json.
