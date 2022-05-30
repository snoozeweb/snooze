## v1.5.0

### New features
* Web: Cliking on the main graph in Dashboard redirects to the corresponding alerts
* Web: Alerts preview when writing a condition
* Core: Support for Grafana 8.5+ (same webhook)

### Changes
* Web: Updated all web packages + NodeJS (10->12)
* Web: Enabled/Disabled labels replaced with Checkmark/Crossmark

### Bug fixes
* Core: DB query typo in Actions
* Core: Batch form would not being displayed if no action was previously created

## v1.4.1

### New features
* Web: Added a frequency display in Notifications
* Web: Added a batch display in Actions
* Core: Monitoring endpoint at `/api/health`
* Core: Nagios/Icinga compatible check script (`check_snooze_server`)

### Changes
* Core: Code linting and adding type hints
* Core: Now pre-catching all database errors to give more information about what
  was the query before throwing an exception
* Core: Backups can now fail independently on a per-collection basis

### Bug fixes
* Web: Bad display for Sunday
* Web: Sort weekdays
* Web: Could not reset Conditions right member correctly
* Core: Improving the thread management to prevent rogue threads dying without causing Snooze
  to die as well.
* Core: Fixing an issue related to the URL character limit when passing the connection string
  to kombu. Now it is using a patched transport backend that passes MongoClient()[database]
  directly.
* Core: Making sure batched actions are not out to date
* Core: TinyDB Audit was broken
* Core: Increasing log file size from 1MB to 100MB
* Core: Catching issues better within Action thread
* Core: Pretty big typo in Action class

## v1.4.0

### New features
* Web: Custom message for no alerts
* Web: Show current version in Status
* Core: Supports batched actions
* Core: Audit logs
* Core: Supports time constraints over midnight
* Core: Added daily backups
* Core: Prevent alerts flapping
* Env: Switched from pyenv to poetry
### Bug fixes
* Web: Removed CoreUI Collapse component
* Web: Resets current page number when changing tabs
* Web: Sunday was numbered as 7 instead of 6
* Web: Trim tags
* Web: Time related filters correctly updated on refresh
* Web: Fixed datetime on keyboard input
* Web: Fixed modals bouncing unexpectedly
* Core: (!=) Condition will not assume the field exists
* Core: Properly delete discarded logs
* Core: Fixed a concurrency issue when reloading plugins
* Core: Fixed an issue with IN operator for TinyDB
* Core: Prevent rejecting all PUT and POST data if only one is failing

## v1.3.0

### New features
* Core/Web: Better handling of strings in conditions and modifications
* Core/Web: Supports AND/OR condititions with more than 2 arguments
* Core: New Key-values modification (add fields to an alert based on matching a dictionary)
* Core: Added rotating logs in /var/log/snooze/snooze-server.log
* Core: Added `notification_from` field to Alerts when they get re-escalated
* Core: Resend failed notifications (configurable in Settings)
* Core: Supports prometheus-client 13.x
### Bug fixes
* Web: Fixed a display error when deleting part of a condition
* Web: Active and Upcoming Snooze filters/Notifications were sometimes wrong
* Web: Supports history for sorting and paging
* Core: Avoid loading in memory unnecessary plugin data
* Core: Fixed an issue with duplicate policies using Replace (lost UID)
* Core: Better handling of crashed conditions and modifications
* Core: Fixed a Time Constraints issue with exact matches
* Core: Triggered notifications in an alert were capped at one item
* Core: Metrics api endpoint failed to return sometimes
* Core: Do not retry all actions if only one fails
* Core: Fixed memory issue with comment related queries

## v1.2.0

### New features
* Web: Better display for some tables
* Web: Better display for Modifications
* Web: Set tables to a busy state for each request
* Core: RegexSub (useful for improving aggregation or scrapping secrets)
* Core: Prometheus webhook added
### Bug fixes
* Web: Could not clear search if the bar was empty
* Web: Improved Widget + Environment bar display
* Web: Few display issues
* Web: Modals and Toasts were not disappearing once faded out
* Web: Time in Time Constaints was reset when updating
* Web: Snooze filters Retro apply modal was not showing up
* Core: Conditions refactoring

## v1.1.2

### New features
* Web: Added Copy selection in tables context menu
* Web: Added Search selection in tables context menu
### Bug fixes
* Web: Values in Modifications were not correctly retrieved in edit mode
* Web: Mail and Grafana Infos wre not correctly ported to CoreUI 4.x
* Core: Grafana webhook did not work correctly if tags were empty
* Core: Conditions were not working if they were null
* Core: Receiving multiple OK for the same alert now processes the first one only

## v1.1.1

### Bug fixes
* Core: Forced Pymongo < 4.0

## v1.1.0

### New features
* Web: Updated from Vue 2.x to 3.x
* Web: Updated CoreUI from 3.x to 4.x
* Web: Removed Bootstrap dependency
* Web: Converted Radio buttons to Switches
* Web: Added row selector to Tables
* Core: Added alert_closed metric
* Core: Added SNOOZE_CLUSTER env variable
* Core: Separated alerts and comments housekeeping
* Core: New modification: Regex Parse
* Added full container deployment (docker-compose.yaml)
### Bug fixes
* Core: Could get duplicates if multiple servers were bootstraped at the same time

## v1.0.17

### New features
* WebUI Settings: configure severity levels that automatically close alerts
* Can now configure WebUI tables directly from config files
* Housekeeper: Also cleanup expired notifications
### Bug fixes
* JWT Tokens were not functioning properly
* Retro actively apply Snooze filters were throwing error messages if no change was made
* CONTAINS and IN conditions were not working properly if an alert value was empty
* Stats dashboard stored in TinyDB had chances to lock the DB when being displayed

## v1.0.16

### Bug fixes
* TinyDB was broken since v1.0.11
* Date was handled incorrectly for TinyDB metric features
* Github CI fix

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
