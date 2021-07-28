#!/usr/bin/python3.6


import json
from snooze.utils import Condition

from logging import getLogger
log = getLogger('snooze.notification')

from snooze.plugins.core import Plugin

class Notification(Plugin):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)

    def process(self, record):
        for notification in self.notifications:
            if notification.enabled and notification.condition.match(record):
                name = notification.name
                log.debug("Matched notification `{}` with {}".format(name, record))
                if len(notification.action_plugins) > 0:
                    for action_plugin in notification.action_plugins:
                        if not 'notifications' in record:
                            record['notifications'] = []
                            record['notifications'].append(name)
                        action = action_plugin.get('action')
                        action_content = action_plugin.get('content', {})
                        action_name = action_content.get('action_name', '')
                        try:
                            action.send(record, action_content)
                            self.core.stats.inc('notification_sent', {'name': notification.name, 'action': action_name})
                        except Exception as e:
                            self.core.stats.inc('notification_error', {'name': notification.name, 'action': action_name})
                            log.error("Notification {} action '{}' could not be send".format(self.name, action_name))
                            log.exception(e)
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
        self.actions = notification.get('actions', [])
        self.action_plugins = []
        if (type(self.actions) is list) and len(self.actions) > 0:
            query = ['IN', self.actions, 'name']
            action_search = core.db.search('action', query)
            if action_search['count'] > 0:
                actions_data = action_search['data']
                for action_data in actions_data:
                    action = action_data.get('action', {})
                    content = action.get('subcontent', {})
                    content['action_name'] = action_data.get('name')
                    self.action_plugins.append({'action': core.get_action_plugin(action.get('selected')), 'content': content})
            elif self.enabled:
                log.error("Could not find any action defined notification {}. Disabling".format(self.name))
                self.enabled = False
        elif self.enabled:
            log.error("Notification {} has no action. Disabling".format(self.name))
            self.enabled = False
