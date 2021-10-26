## v1.0.15

### New features
* Storing metrics locally and displaying a dashboard
* Can configure a default landing page in preferences
* Keeping track of Last login for all users
* InfluxDB 2.0 webhook added
### Bug fixes
* Do no crash whenever a plugin fails to load
* Widgets pretty print was not working properly
* Failed webhook actions did not register as failed properly

## v1.0.14

### New features
* External core plugins support
* Added a spinner in the webUI when doing a DB query
* Search in Alerts should be faster
* Resized Condition box to get more input space
* Snooze filters can discard alerts
* Retro apply Snooze filters to all alerts
### Bug fixes
* Going back to wsgiref. It was working fine. Waitress is just having issues with TLS

## v1.0.13

### Bug fixes
* Fixed issues from previous version about Waitress
* Fixed CI to account for pypi delay before building docker image

## v1.0.12

### New features
* Kapacitor webhook added
* LDAP: Filtering out groups with group_dn or base_dn
* Moving Unix socket management out of the falcon API
* Using Waitress for Unix socket and TCP socket
* Secrets are now bootstrapped using random numbers and are stored in the backend database
* Dedicated middleware for logging
### Bug fixes
* When changing tabs or refreshing, webUI row tables are not flickering anymore
* Throttled alerts generated duplicate entries
* Aggregated alerts now correctly reset their snooze filters fields

## v1.0.11

### New features
* Environmnents support! Can be used to create search filters that can be applied on top of any search
### Bug fixes
* Wrong version of PyJWT broke LDAP auth
* Recent change in plugin loading broke plugin processing order

## v1.0.10

### New features
* Config option to disable authentication. People will be automatically logged in as root
* Anonymous login backend. Can be enabled in Settings (or general.yaml config file)
* Debian package export
* Webhooks support
* Grafana webhook added
* Copy content from any row in the WebUI
### Changes
* Plugin refactor. Now even actions are considered core plugins. Scanning snooze/plugins/core folder instead of declaring plugins in core.yaml
* Moved Patlite plugin to [snooze\_plugins](https://github.com/snoozeweb/snooze_plugins) repository
### Bug fixes
* Default authentication backend display order not being respected since 2021-06-30

## v1.0.9 (2021-09-04)

* Admins can use the webUI to manually trigger alerts
* Added a toggleable button to automatically refresh Alerts display
* Log in back to the webUI now keeps the initial query

## v1.0.8 (2021-08-27)

* Advanced schedule support for Notifications (number of notifications sent, frequency, delay)
* More environment variables supported (documentation to come later)
* Can now pass full Record to webhooks using {{ __self__ }} (Jinja template)
* New Search bar for the WebUI with a powerful [query language](https://github.com/snoozeweb/snooze/blob/master/doc/14_Query_language.md) supported
* Dockerfile added. Snooze image to come very soon!
* When re-escalating an alert, can now trigger Modifications. Any actual change to a Record will trigger Notifications again
* Can now use Jinja templates in Modifications (Rules, Re-escalations)
* Housekeeper will auto cleanup expired Snooze filters. Parameters supported
* New view for the Alert Infos tab

## v1.0.7 (2021-08-05)

* New feature: Time constraint for notifications. Same as for Snooze filters
* New feature: Delay for notifications. If an alert gets acknowledged or closed before the delay ends, it does not get notified.
* New feature: Watchlist for aggregate rules. Bypass aggregation if a specified field gets updated
* New feature: Webhooks now support CA bundles

## v1.0.6 (2021-07-29)

* Webhook fixes
* Added a new feature to webhooks: can now inject HTTP Response to a Record
* Fixes issue with Conditions NOT and EXISTS not being properly displayed

## v1.0.5 (2021-07-27)

* Fixed bugs with aggregates from previous release
* Reworked alerts lifecycle. Alerts first show up without a state. "open" state can now be entered only whenever reopening a closed alert by user interaction or automatically whenever a closed alert receives a new aggregation
* New action: Webhook! Can be used by Notification to call a URL. Documentation will come soon

## v1.0.4 (2021-07-26)

Transferred Aggregates logic to Records, meaning there is one less collection in the DB and one less menu item to care about. As a bonus, now whenever an aggregated record gets alerted, if the aggregate state was "open" or "ack", it will get automatically re-escalated (before it was creating a new alert)

## v1.0.3 (2021-07-20)

* Widgets
* Records lifecycle (open/close)
* New Snooze filters time constraints (datetime, time, weekdays). Can be mixed together
* Patlite support
* More documentation
* Bugfixes

## v1.0.2 (2021-07-09)

Fixes

## v1.0.0 (2021-07-06)

Initial release
