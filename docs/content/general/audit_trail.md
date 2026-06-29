---
sidebar_position: 16.5
---

# Audit trail

## Overview

Snooze keeps a queryable audit trail in the `audit` collection. Two kinds of
events are recorded:

- **CRUD mutations** — every create / replace / patch / delete of an
  audited object (rules, aggregate rules, snoozes, users, roles, settings…)
  writes a row describing who changed what.
- **Authentication events** — successful and failed logins, token refreshes,
  and logouts. These give operators a security trail that previously existed
  only as raw HTTP request-log lines.

Each row carries an `object_type` field. For CRUD mutations it is the name of
the affected collection (`rule`, `aggregaterule`, `user`, `role`, …). For
authentication events it is the sentinel value `auth`.

## Auth event rows

Auth rows use the `object_type: auth` sentinel and set `action` to one of:

| `action`       | When                                                          |
|----------------|---------------------------------------------------------------|
| `login`        | A successful login (local, LDAP, anonymous, or SSO/OIDC).     |
| `login_failed` | A rejected login: bad credentials, disabled backend/account, failed SSO exchange, or a failed token refresh. |
| `refresh`      | A refresh token was successfully exchanged for a new pair.    |
| `logout`       | A logout request (the refresh token is revoked).              |

The `username` field records the attempted identity and `method` the auth
backend (`local`, `ldap`, `anonymous`, `oidc`, …). For `login_failed` the
attempted username is recorded on purpose — the HTTP response still returns a
generic "invalid credentials" message, but the audit row is operator-only and
is standard security-log practice. Logout does **not** verify the token before
revoking it, so its identity is unknown: logout rows carry an empty `username`.

## Querying

Auth events are read through the same `/api/v1/audit` surface as CRUD audit
rows, and can be filtered with the [Snooze query language](./querylanguage.md).
For example, to find every failed login:

``` console
object_type = auth and action = login_failed
```

To narrow to a single user's authentication history:

``` console
object_type = auth and username = alice
```

## Retention

Audit rows are subject to the housekeeper's audit retention, configured by the
`cleanup_audit` setting (see [Housekeeping](./housekeeping.md) and the
[housekeeper configuration reference](../configuration/housekeeping.md)). To
disable the audit trail entirely, set the audit retention to `0`.
