---
sidebar_position: 3
---

# Auth configuration

> Package location  
> `/etc/snooze/server-go/auth.yaml` (Go canonical)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (restart the server to re-read)

Static knobs for the JWT session tokens that Snooze mints on login and verifies on every authenticated request. Every field is optional and has a sane default — an empty file (or no file at all) yields HS256 tokens with a 1-hour access lease and a 7-day refresh lease, signed with a key auto-generated in the database on first boot.

The one field worth setting in production is `token_secret`: pin it to a stable, operator-controlled value so the signing key survives a rotation of the `secrets` collection and stays identical across cluster nodes.

This page covers only the **token** mechanics. The authentication **backends** (who is allowed to log in) live elsewhere: see [LDAP configuration](./ldap_auth.md) and [OIDC authentication](./oidc_auth.md).

The Go schema lives in `internal/config/schema/auth.go`.

## Properties

### token_secret

> Type  
> string (≥ 32 bytes)
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_TOKEN_SECRET`
>
> Default  
> `""` (auto-generate a key in the database on first boot)
>
> HS256 signing key for session JWTs. When empty (the default), the bootstrap generates a random key, stores it in the `secrets` collection, and reuses it across restarts. When set, it **overrides** that DB-seeded key, so the signing key is stable and operator-controlled across rotations of the secrets collection and identical on every replica.
>
> The value must be **at least 32 bytes** (the SHA-256 block size). A shorter secret fails the boot with `boot: auth.token_secret must be at least 32 bytes (got N)` — the server does not start. Because it is a secret, prefer the environment variable over the YAML file and keep it out of version control.

### token_algorithm

> Type  
> string (`'HS256'`)
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_TOKEN_ALGORITHM`
>
> Default  
> `'HS256'`
>
> JWT signing algorithm. `HS256` is the only supported value; any other value is rejected at startup. Present for forward compatibility — leave it at the default.

### token_lease

> Type  
> Duration
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_TOKEN_LEASE`
>
> Default  
> `1h`
>
> Lifetime of an access token. After it expires the client exchanges its refresh token for a fresh access token. A zero or negative value falls back to `1h`.

### refresh_token_lease

> Type  
> Duration
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_REFRESH_TOKEN_LEASE`
>
> Default  
> `168h` (7 days)
>
> Lifetime of a refresh token. Refresh tokens are stored hashed in the `refresh_token` collection; once this lease elapses the user must log in again.

### token_issuer

> Type  
> string
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_TOKEN_ISSUER`
>
> Default  
> `'snooze'`
>
> Value of the `iss` claim written into minted tokens and checked on verification.

### token_audience

> Type  
> string (comma-separated)
>
> Environment variable  
> `SNOOZE_SERVER_AUTH_TOKEN_AUDIENCE`
>
> Default  
> `'snooze'`
>
> Value(s) of the `aud` claim. Treated as a comma-separated list following the OAuth/OIDC convention; an empty value falls back to `snooze`.

## Example

``` yaml
---
# All fields optional. The only one worth setting in production is
# token_secret — and you should prefer SNOOZE_SERVER_AUTH_TOKEN_SECRET
# over committing it to a file.
token_secret: "a-stable-operator-supplied-key-at-least-32-bytes-long"
token_algorithm: HS256
token_lease: 1h
refresh_token_lease: 168h
token_issuer: snooze
token_audience: snooze
```
