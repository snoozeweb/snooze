# Rules

![Architecture](images/architecture.png)

Can add, modify or delete fields from a Record.
Records have to match the Rule's condition in order to being processed.
Rules are very useful to analyze incoming Records and add infos that were not in the original log.

For example:
```yaml
# Record before being processed by Rule
host: prod-syslog01.example.com
```
```yaml
# Rule
name: is_production
condition: host MATCHES ^prod.*
modification: SET environment = production
```
```yaml
# Record after being processed by Rule
host: prod-syslog01.example.com
rules: ['is_production']
environment: production
```
Any Record matching a Rule will have a new field `rules` added with the list of matched Rules.

Rules can have an optional field called `children` which can hold a list of Rules. These Rules will be processed the same way Rules are but only if the parent's condition has been correctly matched in the first place.
This design allowing Rules to be nested is very convenient to avoid repeating the same conditions across multiple Rules.
