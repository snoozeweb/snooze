# Notifications

![Architecture](images/architecture.png)

Can call a list of Actions which are alerting scripts.
Records have to match the Notification's condition in order to being processed.
Notification is the only component relying on another one. Indeed, at least one Action has to be created first before being able to use it.

For example:

```yaml
# Record before being processed by Notification
host: prod-syslog01.example.com
rules: ['is_production']
environment: production
```
```yaml
# Notification
name: alert_production
condition: environment = production
actions: ['sendmail_all'] # Assumes this action already exists
```
```yaml
# Record after being processed by Notification
host: prod-syslog01.example.com
rules: ['is_production']
environment: production
notifications: ['alert_production']
```

The Record matched the Notification, therefore the action `sendmail_all` will be called.
Any Record matching a Notification will have a new field `notifications` added with the list of matched Notifications.
