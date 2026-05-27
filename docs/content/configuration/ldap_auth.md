---
sidebar_position: 3
---

# LDAP configuration

> Package location  
> `/etc/snooze/server-go/ldap.yaml` (Go canonical)
>
> Legacy name  
> `/etc/snooze/server/ldap_auth.yaml` (still loaded)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (restart the server to re-read)

Configuration for the LDAP authentication backend. Only consulted when `enabled: true`; the LDAP provider is otherwise not registered in `buildAuthProviders`.

The Go schema lives in `internal/config/schema/ldap.go`.

## Properties

### enabled

> Type  
> boolean
>
> Default  
> `False`
>
> Enable or disable LDAP Authentication

### base_dn

> Type  
> string
>
> LDAP users location. Multiple DNs can be added if separated by semicolons

### user_filter

> Type  
> string
>
> LDAP search filter for the base DN
>
> <div class="admonition">
>
> Example 1
>
> ``` yaml
> user_filter: (objectClass=posixAccount)
> ```
>
> </div>

### bind_dn

> Type  
> string
>
> Distinguished name to bind to the LDAP server
>
> <div class="admonition">
>
> Example 1
>
> ``` yaml
> bind_dn: CN=john.doe,OU=users,DC=example,DC=com
> ```
>
> </div>

### bind_password

> Type  
> string
>
> Environment variable  
> `LDAP_BIND_PASSWORD`
>
> Password for the Bind DN user

### host

> Type  
> string
>
> LDAP host
>
> <div class="admonition">
>
> Example 1
>
> ``` yaml
> host: ldaps://example.com
> ```
>
> </div>

### port

> Type  
> integer
>
> Default  
> `636`
>
> LDAP server port

### group_dn

> Type  
> string
>
> Base DN used to filter out groups. Will default to the User base DN Multiple DNs can be added if separated by semicolons

### email_attribute

> Type  
> string
>
> Default  
> `'mail'`
>
> User attribute that displays the user email adress

### display_name_attribute

> Type  
> string
>
> Default  
> `'cn'`
>
> User attribute that displays the user real name

### member_attribute

> Type  
> string
>
> Default  
> `'memberof'`
>
> Member attribute that displays groups membership

