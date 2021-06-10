#!/usr/bin/python3.6

import json
from subprocess import run, CalledProcessError, PIPE
from snooze.utils import Condition
from snooze.utils.functions import dig
from jinja2 import Template

from logging import getLogger
log = getLogger('snooze.notification')

from snooze.plugins.core import Plugin

class Notification(Plugin):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.core.stats.init('notification_sent')
        self.core.stats.init('notification_error')

    def process(self, record):
        for notification in self.notifications:
            if notification.enabled and notification.condition.match(record):
                name = notification.name
                log.debug("Matched notification `{}` with {}".format(name, record))
                if notification.interpret_fields:
                    interpret_jinja(notification, record)
                notification_arguments = record.copy()
                notification_arguments.update({'notification': notification})
                script = [notification.command]
                arguments = notification.arguments
                for argument in arguments:
                    if type(argument) is str:
                        script.append(argument)
                    if type(argument) is list:
                        script += argument
                    if type(argument) is dict:
                        script += sum([ [k, v] for k, v in argument])
                try:
                    log.debug("Will execute notification script `{}`".format(' '.join(script)))
                    stdin = json.dumps(notification_arguments) if notification.json else None
                    process = run(script, stdout=PIPE, input=stdin, encoding='ascii')
                    log.debug('stdout: ' + str(process.stdout))
                    log.debug('stderr: ' + str(process.stderr))
                    self.core.stats.inc('notification_sent', {'name': name})
                except CalledProcessError as e:
                    self.core.stats.inc('notification_error', {'name': name})
                    log.error("Notification {} could not run `{}`. STDIN = {}, {}".format(
                        name, ' '.join(script), stdin, e.output)
                    )
        return record

    def reload_data(self):
        super().reload_data()
        self.notifications = []
        for f in (self.data or []):
            self.notifications.append(NotificationObject(f))

class NotificationObject():
    def __init__(self, notification):
        self.enabled = notification.get('enabled', True)
        self.name = notification['name']
        self.condition = Condition(notification.get('condition'))
        self.command = notification.get('command')
        self.arguments = notification.get('arguments', [])
        self.interpret_fields = notification.get('interpret_fields')
        self.json = notification.get('json')

def interpret_jinja(notification, record):
    fields = notification.interpret_fields
    for field in fields:
        field_value = dig(notification, field)
        if field_value:
            interpreted = Template(field_value).render(record.copy())
            notification[field] = interpreted
            log.debug("Interpreting field {}: `{}` as `{}`".format(field, field_value, interpreted))
