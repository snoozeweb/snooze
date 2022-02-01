# Snooze roadmap

## Short term

* Refactor configuration management (using `python-confuse` maybe)
* Rewrite DB path
* Replace list of Rules with a Root Rules (Ex: To handle global actions such as maintenance)
* Transform a record search into a rule
* Export and import DB
* When restarting snooze server, should not have to restart syslog as well
* Client CLI
  * Client CLI dynamically updated with new versions of the API schema
* Remove bootstrap and bootstrap-vue packages (because coreui ships them already)
* Plugin manager (wraps Pip)
* Play with montydb (remove tinydb, replace mongomock)
* Audit (who did what at any time)
* Time constraints: holidays (use a custom calendar?)
* Time constraints in rules
* Personal environment
* Re-escalation message to Google Chat
* Recode severity to map fixed values

## Long term

* TCP stream for resources that can be updated. On update, snooze-server should send a
push notification to all listening clients.
* Replace http basic auth with digest auth
* Vagrant testing
* LDAP backend using SASL (GSSAPI)
* Auto-generated certificates
* Refactor testing to use the same samples for all tests
  * Standardize testing for modules
* Use tox for supporting multiple versions of python
* When clicking on a rule row, redirect to Records and apply a search listing all records matching this rule (and all parents conditions as well)
* Dedicated view for each record by clicking on it (or have a button)
  * Basically replace "More" by a dedicated view (remove it or rename it)
* When filling up a Condition, make it more user friendly by showing a dropdown of suggestions as you type
* Snooze notify
  * Be able to optionally assign one or multiple commands when creating a snooze filter (using a dropdown like for notifications)
  * Before 'Abort and Write to DB' when a record is snoozed, run these commands
* Add auto documentation for each known error log received. Possibility for the user to add more
