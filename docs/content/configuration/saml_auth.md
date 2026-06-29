---
sidebar_position: 4.6
---

# SAML2 authentication (ADFS / Okta / Shibboleth / PingFederate)

> Package location
> `/etc/snooze/server-go/saml.yaml` (Go canonical)
>
> Loader
> `internal/config` (koanf) — **file config only**
>
> Live reload
> **None.** SAML carries the SP signing key path + cert (infra tier) and the
> IdP-registered ACS URL, which must not change without a metadata re-exchange.
> Edits require a restart.

Snooze ships a SAML2 **SP-initiated** Single Sign-On backend that works with any
compliant identity provider (ADFS, Okta, Shibboleth, PingFederate, Entra ID's
SAML mode). A user clicks the **SAML** tab on the login page, is redirected to
the IdP, signs in, and the IdP POSTs a signed assertion back to Snooze's
Assertion Consumer Service (ACS). Snooze maps the assertion's NameID +
group/role attributes to a Snooze identity, JIT-provisions the user, and mints
the normal session JWT pair.

The backend is **disabled by default** and only registered when
`saml.enabled: true`. When enabled it mounts three public routes (`{method}` is
the configured `method`, default `saml`):

- `GET  /api/v1/login/{method}/start` — generates a RelayState, sets a
  short-lived signed cookie, and 302-redirects the browser to the IdP SSO
  endpoint (HTTP-Redirect binding).
- `POST /api/v1/login/{method}/acs` — the **Assertion Consumer Service**.
  Consumes the IdP's `SAMLResponse` form POST, constant-time compares the echoed
  `RelayState` against the cookie (CSRF defence), validates the assertion
  (signature, audience, conditions, `NotOnOrAfter`), maps it to an identity, and
  issues a session.
- `GET  /api/v1/login/{method}/metadata` — serves the SP `EntityDescriptor` XML
  for the IdP administrator to register Snooze in one paste.

Unlike OIDC (an OAuth authorization-code flow), SAML's IdP replies by **POSTing**
a signed assertion to the ACS — so it is a distinct interface, not the OIDC
redirect provider.

## Registering Snooze with your IdP

1. Set the bootstrap config (below), at minimum `enabled`, an IdP metadata
   source, and `acs_url`, then restart `snooze-server`.
2. Fetch the SP metadata from `GET /api/v1/login/saml/metadata` and hand it to
   your IdP administrator (or paste the `EntityDescriptor` into the IdP's
   "import SP metadata" field). It lists the ACS endpoint (and the SP signing
   certificate, if you configured one).
3. The ACS URL you register on the IdP **must** match `acs_url` exactly.

## SP signing key / cert (optional)

A signing key is only needed when `sign_requests: true` (the SP signs its
AuthnRequests) or when the IdP **encrypts** assertions (the SP decrypts them).
An IdP that sends unencrypted assertions and does not require signed requests
needs no SP key.

Generate a self-signed SP key pair:

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout sp.key -out sp.crt -subj "/CN=snooze-sp"
```

Then point `sp_key_file` / `sp_cert_file` at the PEM files.

## Group → role mapping

A SAML user receives **only** the roles that group→role resolution grants — a
JIT-provisioned user carries no roles of its own, so it cannot self-escalate.
Map the assertion's group/role attribute values to Snooze roles in the
**Roles** UI (each role has a `groups[]` list intersected with the user's
groups). There is no separate allow-group gate: a user whose groups map to no
role ends up with an empty permission set and effectively no access — the same
model as OIDC.

On a fresh database the seeded **admin** role's `groups[]` is populated with
`admin_role_value`, so a turnkey deployment can grant admin to a single IdP
group value out of the box.

## Multitenancy

The tenant slug rides in the RelayState cookie (`org`, from the `/start` query
string); absent → the default tenant. The tenant is checked **before** any token
is minted. Attribute-based tenant resolution from the assertion is a separate
feature and is not done here.

## Properties

### enabled

> Type
> boolean
>
> Default
> `false`
>
> Enable or disable the SAML backend. When `false` the routes are not mounted
> and the backend does not appear in `GET /api/v1/login`.

### idp_metadata_url

> Type
> string
>
> A remote URL serving the IdP `EntityDescriptor`. Fetched once on first use.
> Provide **either** this or `idp_metadata_xml`.

### idp_metadata_xml

> Type
> string
>
> Inline alternative to `idp_metadata_url`: either a path to a local metadata
> file or the literal `EntityDescriptor` XML.

### acs_url

> Type
> string (required when enabled)
>
> The absolute Assertion Consumer Service URL registered on the IdP, e.g.
> `https://snooze.example.com/api/v1/login/saml/acs`.

### entity_id

> Type
> string
>
> The SP entityID advertised in the metadata. Defaults to the ACS URL when
> empty.

### sp_cert_file / sp_key_file

> Type
> string
>
> PEM paths to the SP signing/decryption certificate and private key. Required
> only when `sign_requests` is true or the IdP encrypts assertions.

### sign_requests

> Type
> boolean
>
> Default
> `false`
>
> When true the SP signs its AuthnRequests (needs `sp_key_file` / `sp_cert_file`)
> and advertises `AuthnRequestsSigned` in the metadata.

### want_assertions_signed

> Type
> boolean
>
> Default
> `true`
>
> Advertises `WantAssertionsSigned` in the SP metadata. The library always
> requires a signed assertion regardless of this flag.

### allow_unsolicited

> Type
> boolean
>
> Default
> `false`
>
> Tolerate IdP-initiated (unsolicited) responses. The safer default is `false`:
> the ACS requires a matching RelayState cookie, so a stray IdP-initiated POST is
> rejected.

### groups_attribute

> Type
> string
>
> Default
> `groups`
>
> The assertion attribute whose values feed `Identity.Groups`.

### roles_attribute

> Type
> string
>
> An optional second attribute whose values are unioned into `Identity.Groups`
> (parity with OIDC's `roles_claim`).

### method

> Type
> string
>
> Default
> `saml`
>
> The identity method + login URL segment + JWT method claim. **Not**
> runtime-editable — changing it orphans provisioned users and breaks the ACS
> route registered on the IdP.

### display_name / icon

> Type
> string
>
> Defaults
> `SAML` / `saml`
>
> The login button label and icon key.

### admin_role_value

> Type
> string
>
> Default
> `Admin`
>
> The group/role attribute value that maps to the Snooze `admin` role. On a
> fresh DB the seeded admin role's `groups[]` is populated with this value.

## Example

```yaml
# /etc/snooze/server-go/saml.yaml
enabled: true
idp_metadata_url: https://idp.example.com/federationmetadata/2007-06/federationmetadata.xml
acs_url: https://snooze.example.com/api/v1/login/saml/acs
entity_id: https://snooze.example.com/saml
groups_attribute: http://schemas.xmlsoap.org/claims/Group
roles_attribute: Role
admin_role_value: Snooze-Admins
display_name: Corporate SSO
# Only needed if the IdP requires signed requests or encrypts assertions:
# sign_requests: true
# sp_cert_file: /etc/snooze/sp.crt
# sp_key_file: /etc/snooze/sp.key
```

The Go schema lives in `internal/config/schema/saml.go`.
