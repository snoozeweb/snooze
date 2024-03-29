#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Plugin for handling notifications. Notification trigger actions (sending mail, webhook, script, ...)
based on a rule (time constraint, condition)'''


from logging import getLogger
from typing import List

from snooze.plugins.core import Plugin
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.time_constraints import get_record_date, init_time_constraints

proclog = getLogger('snooze-process')
apilog = getLogger('snooze-api')

class Notification(Plugin):
    '''Core plugin for notifications'''
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.action = None
        self.notifications: List[NotificationObject] = []

    def process(self, record):
        proclog.debug('Start')
        for notification in self.notifications:
            if notification.enabled and record.get('state') not in ['ack', 'close'] and notification.match(record):
                proclog.info("Notification %s matched record", notification.name)
                notification.send(record)
        proclog.debug('Done')
        return record

    def validate(self, obj):
        '''Validate a notification object'''
        validate_condition(obj)

    def post_init(self):
        pass

    def _post_reload(self):
        if not self.action:
            self.action = self.core.get_core_plugin('action')
        notifications = []
        for notification in (self.data or []):
            notifications.append(NotificationObject(notification, self))
        self.notifications = notifications

class NotificationObject:
    '''An object representing a single notification in the database'''
    def __init__(self, notification, plugin):
        self.notification = plugin
        self.core = plugin.core
        self.uid = notification.get('uid')
        self.enabled = notification.get('enabled', True)
        self.name = notification['name']
        self.condition = get_condition(notification.get('condition'))
        self.freq = notification.get('frequency', {})
        self.total = self.freq.get('total', 1)
        self.delay = self.freq.get('delay', 0)
        self.every = self.freq.get('every', 0)
        self.actions = notification.get('actions', [])
        self.action_plugins = []
        self.options = {}
        if isinstance(self.actions, list) and len(self.actions) > 0:
            if self.actions:
                self.action_plugins = [a for a in self.notification.action.actions if a.name in self.actions]
            elif self.enabled:
                apilog.warning("No action defined in '%s', disabling notification", self.name)
                self.enabled = False
        elif self.enabled:
            apilog.warning("No action defined in '%s', disabling notification", self.name)
            self.enabled = False
        apilog.debug("Initialized with action plugins: %s", [str(a) for a in self.action_plugins])
        # Initializing the time constraints
        apilog.debug("Init Notification filter %s Time Constraints", self.name)
        self.time_constraint = init_time_constraints(notification.get('time_constraints', {}))

    def match(self, record):
        '''Whether a record match the Notification object'''
        return self.condition.match(record) and self.time_constraint.match(get_record_date(record))

    def send(self, record):
        '''Trigger the notification'''
        if not 'notifications' in record:
            record['notifications'] = []
        if self.name not in record['notifications']:
            record['notifications'].append(self.name)
        if len(self.action_plugins) > 0:
            if 'notification_retry' in record:
                retry = record['notification_retry']
            elif 'notification_retry' in self.options:
                retry = self.options['notification_retry']
            else:
                retry = self.core.config.notifications.notification_retry

            if 'notification_freq' in record:
                freq = record['notification_freq']
            elif 'notification_freq' in self.options:
                freq = self.options['notification_freq']
            else:
                freq = int(self.core.config.notifications.notification_freq.total_seconds())
            action_obj = {
                'record': record,
                'delay': self.delay,
                'every': self.every,
                'total': self.total,
                'retry': retry,
                'freq': freq,
            }
            self.core.stats.inc('notification_sent', {'name': self.name})
            for action_plugin in self.action_plugins:
                proclog.info("Triggering action '%s'", action_plugin.name)
                action_plugin.send(action_obj)
        else:
            proclog.error("Notification %s has no action. Cannot send", self.name)
