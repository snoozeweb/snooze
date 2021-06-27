#!/usr/bin/python3.6


import json
from copy import deepcopy
from snooze.utils import Condition

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
                if self.action_plugin:
                    if not 'notifications' in record:
                        record['notifications'] = []
                        record['notifications'].append( name)
                    self.action_plugin.send(deepcopy(record), self.content)
                else:
                    log.error("Notification {} has to action. Cannot send".format(self.name))
        return record

    def reload_data(self, sync = False):
        super().reload_data()
        self.notifications = []
        for f in (self.data or []):
            self.notifications.append(NotificationObject(f, self.core))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class NotificationObject():
    def __init__(self, notification, core):
        self.enabled = notification.get('enabled', True)
        self.name = notification['name']
        self.condition = Condition(notification.get('condition'))
        self.action = notification.get('action', {})
        self.action_plugin = next(iter([plug for plug in core.action_plugins if plug.name == self.action.get('selected')]), None)
        self.content = self.action.get('subcontent')
        if self.enabled and not self.action_plugin:
            log.error("Notification {} has to action. Disabling".format(self.name))
            self.enabled = False
