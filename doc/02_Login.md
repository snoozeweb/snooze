# Log in to the web interface

![Login](images/web_login.png)

## URL

By default, the web interface listens locally on port 5200: http://localhost:5200

## Default Local user 'root'

In case `create_root_user` in `/etc/snooze/server/core.yaml` has been left to **true** or is **undefined**, a local user named `root` will be automatically created whenever snooze is run for the first time. Its password is `root` and it has admin privileges.

## JWT Token

It is always possible to generate a root token that can be used for **JWT Token** authentication method if [Snooze Client](https://github.com/snoozeweb/snooze_client) is installed:
```bash
# Run with root or snooze user
$ snooze root-token
Root token: eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjp7Im...
```

## LDAP Authentication

[Configure LDAP Authentication](13_LDAP.md)
