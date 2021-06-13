# Snooze roadmap

## Short term

* HTTPS in falcon
* Refactor configuration management (using `python-confuse` maybe)
* Rewrite DB path
* Make sure the root_token in /var/run is being removed when the program exit
* Replace list of Rules with a Root Rules (Ex: To handle global actions such as maintenance)
* Test and implement Objectpath for records search
* Transform a record search into a rule
* Make it possible to change the port and have an easy dev environment
* Add a MQ
* Export and import DB
* When restarting snooze server, should not have to restart syslog as well

## Long term

* TCP stream for resources that can be updated. On update, snooze-server should send a
push notification to all listening clients.
* Replace http basic auth with digest auth
* Vagrant testing
* Client CLI
  * Client CLI dynamically updated with new versions of the API schema
* LDAP backend using SASL (GSSAPI)
* Auto-generated certificates
* Refactor testing to use the same samples for all tests
  * Standardize testing for modules
* Use tox for supporting multiple versions of python
* Add Timeperiods to notifications
* Alert only during timeperiods
* When clicking on a snooze filter row, redirect to Records and apply a search listing all records matching this snooze
* When clicking on a rule row, redirect to Records and apply a search listing all records matching this rule (and all parents conditions as well)
* Toggle snooze/rules/notification on/off
* Dedicated view for each record by clicking on it (or have a button)
  * Basically replace "More" by a dedicated view (remove it or rename it)
* Users create their own tabs using their own filters in Records
* Search + tabs should modify the url so we can copy/paste searches to share them
* When filling up a Condition, make it more user friendly by showing a dropdown of suggestions as you type
* Snooze notify
  * Be able to optionally assign one or multiple commands when creating a snooze filter (using a dropdown like for notifications)
  * Before 'Abort and Write to DB' when a record is snoozed, run these commands
