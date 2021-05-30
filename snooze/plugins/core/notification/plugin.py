#!/usr/bin/python3.6

import json
from subprocess import run, CalledProcessError, PIPE
from snooze.utils import Condition
from snooze.utils.functions import dig
from jinja2 import Template

from logging import getLogger
log = getLogger('snooze.notification')

from prometheus_client import Counter

from snooze.plugins.core import Plugin

class Notification(Plugin):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.core.stats['snooze_notification_sent'] = Counter(
            'snooze_notification_sent',
            'Counter of notification sent',
            ['name'],
        )
        self.core.stats['snooze_notification_error'] = Counter(
            'snooze_notification_error',
            'Counter of notification that failed',
            ['name'],
        )

    def process(self, record):
        for notification in self.data:
            condition = notification.get('condition')
            if Condition(condition).match(record):
                name = notification.get('name')
                log.debug("Matched notification `{}` with {}".format(name, record))
                if notification.get('interpret_fields'):
                    interpret_jinja(notification, record)
                notification_arguments = record.copy()
                notification_arguments.update({'notification': notification})
                script = [notification.get('script')]
                arguments = notification.get('arguments', [])
                for argument in arguments:
                    if type(argument) is str:
                        script.append(argument)
                    if type(argument) is list:
                        script += argument
                    if type(argument) is dict:
                        script += sum([ [k, v] for k, v in argument])
                try:
                    log.debug("Will execute notification script `{}`".format(' '.join(script)))
                    stdin = json.dumps(notification_arguments) if notification.get('json') else None
                    process = run(script, stdout=PIPE, input=stdin, encoding='ascii')
                    log.debug('stdout: ' + str(process.stdout))
                    log.debug('stderr: ' + str(process.stderr))
                    self.core.stats['snooze_notification_sent'].labels(name=name).inc()
                except CalledProcessError as e:
                    self.core.stats['snooze_notification_error'].labels(name=name).inc()
                    log.error("Notification {} could not run `{}`. STDIN = {}, {}".format(
                        name, ' '.join(script), stdin, e.output)
                    )
        return record

def interpret_jinja(notification, record):
    fields = notification.get('interpret_fields')
    for field in fields:
        field_value = dig(notification, field)
        if field_value:
            interpreted = Template(field_value).render(record.copy())
            notification[field] = interpreted
            log.debug("Interpreting field {}: `{}` as `{}`".format(field, field_value, interpreted))
