---
sidebar_position: 3.7
---

# Server-managed user groups

A **server-managed group** is a named cohort of users — for example `sre` or
`oncall-eu` — that Snooze stores itself, independent of any identity provider.
Assigning a [role](./users.md#roles) to a group grants that role to every member
of the group, so you can manage permissions per team instead of editing each
user's role list one account at a time.

## When to use them

There are two sources of group membership in Snooze:

- **IdP groups** are delivered by the authentication backend on every login —
  LDAP group memberships, or the Entra App Roles in an OIDC token. They are
  read-only from Snooze's point of view and only exist for users who sign in
  through that backend.
- **Server-managed groups** are created and edited inside Snooze. They work for
  **any** auth method, including local accounts, and do not require an IdP.

Server-managed groups are the answer when you run local or LDAP users and want
team-based role assignment without hand-editing each user document. The two
sources are **unioned**: a user who belongs to the LDAP group `ops` *and* the
server-managed group `sre` resolves to the roles granted to both at login.

## Data shape

Each group document carries a name, an optional description, and a list of
members. Members are referenced by `{username, method}` — the same identity key
the user collection uses — because two accounts with the same username can exist
on different auth methods:

```json
{
  "name": "sre",
  "description": "Site Reliability Engineering",
  "members": [
    {"username": "alice", "method": "local"},
    {"username": "bob",   "method": "ldap"}
  ]
}
```

The `(tenant_id, name)` pair is unique within a tenant. Adding a username that
does not correspond to an existing user is allowed — that membership simply
never matches anyone until such a user exists.

## Create a group and add members via the API

Groups use the generic CRUD surface under `/api/v1/group`.

Create a group:

```bash
curl -X POST https://snooze.example.com/api/v1/group \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "name": "sre",
        "description": "Site Reliability Engineering",
        "members": [{"username": "alice", "method": "local"}]
      }'
```

Add or remove members later with a partial update — `PATCH` the full `members`
array to the desired state:

```bash
curl -X PATCH https://snooze.example.com/api/v1/group/$UID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"members": [
        {"username": "alice", "method": "local"},
        {"username": "bob",   "method": "ldap"}
      ]}'
```

Reading the list requires the `ro_group` permission; creating, replacing,
patching, and deleting require `rw_group`.

## How membership translates to a role

Grant a role to the group by listing the group's **name** in the role's
`groups[]` field — exactly the same mechanism used for IdP groups (see
[Roles](./users.md#roles) and [Static Roles](./users.md#static-roles)). For
example, a role:

```json
{"name": "sre-oncall", "permissions": ["can_comment", "rw_snooze"], "groups": ["sre"]}
```

is automatically assigned at login to every member of the `sre` group. At login
Snooze unions every server-managed group the user belongs to into the user's
group set, then matches roles against that set. The user gains the role without
`sre-oncall` ever appearing in their own `roles[]` field.

## Managing groups in the console

Groups are manageable from the web console under **Admin → Groups**
(`/web/admin/groups`). No curl required.

The Groups page shows three columns:

- **Name** — the group's identifier, displayed in monospace.
- **Description** — a free-text note (shown as `—` when empty).
- **Members** — a count badge (`N members`) for the number of `{username, method}` pairs.

### Adding a member

Open a group by clicking its row (or click **New** to create one). In the editor
drawer, scroll to the **Members** section. Use the inline add form:

1. Type a **username** in the text input.
2. Select a **method** from the dropdown:
   - `local` — for local-password accounts.
   - `ldap` — for accounts authenticated via an LDAP directory.
   - **Other…** — reveals a free-form text field for OIDC methods such as
     `microsoft` or `google`.
3. Click **Add member**. The pair appears in the member list immediately.

Adding the same `{username, method}` pair twice is a no-op — duplicates are
silently de-duplicated client-side.

### Removing a member

Each member row has a remove button (×). Click it to remove the pair from the
list. The change is not persisted until you click **Save**.

### Membership takes effect on next login

Group membership is resolved at authentication time. Adding a user to a group
grants the associated role(s) on their **next login or token refresh**, not
immediately for an already-authenticated session.

## Notes

- **Deleting a group** never touches any user document. Members simply lose that
  group's membership — and any roles it granted — at their next login.
- **Group membership augments IdP groups; it never replaces them.** A user keeps
  every role from their IdP groups *and* every role from their server-managed
  group memberships.
- A transient database error during the group scan is non-fatal: the user can
  still authenticate and receives roles from their direct `roles[]` field and
  IdP groups. Server-managed group roles are restored on the next successful
  login.
