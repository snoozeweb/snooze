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
                if notification.action_plugin:
                    if not 'notifications' in record:
                        record['notifications'] = []
                        record['notifications'].append( name)
                    notification.action_plugin.send(deepcopy(record), notification.content)
                else:
                    log.error("Notification {} has no action. Cannot send".format(self.name))
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
        self.action = notification.get('action', '')
        action_search = core.db.search('action',['=', 'name', self.action])
        if action_search['count'] > 0:
            action_data = action_search['data'][0]
            action = action_data.get('action', {})
            self.action_plugin = core.get_action_plugin(action.get('selected'))
            self.content = action.get('subcontent', {})
            self.content['notification_name'] = self.name
            self.content['action_name'] = action.get('name')
        elif self.enabled:
            log.error("Notification {} has no action. Disabling".format(self.name))
            self.enabled = False
