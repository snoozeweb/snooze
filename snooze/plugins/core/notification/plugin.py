#!/usr/bin/python3.6


import json
import time
import threading
import hashlib
import socket
from snooze.utils import Condition
from snooze.utils.time_constraints import get_record_date, init_time_constraints

from logging import getLogger
log = getLogger('snooze.notification')

from snooze.plugins.core import Plugin

class Notification(Plugin):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.hostname = socket.gethostname()
        self.thread = NotificationThread(self)
        self.thread.start()
        delayed_notifs = self.core.db.search('notification.delay', ['=', 'host', self.hostname])
        if delayed_notifs['count'] > 0:
            for delayed_notif in delayed_notifs['data']:
                notif_uid = delayed_notif['notification_uid']
                notification = next(notif for notif in self.notifications if notif.uid == delayed_notif['notification_uid'])
                record = delayed_notif['record']
                self.thread.delayed[record['hash']] = {'notification': notification, 'record': record, 'time': time.time() + notification.delay}
            log.debug("Restored notification queue {}".format(self.thread.delayed))

    def process(self, record):
        for notification in self.notifications:
            if notification.enabled and notification.match(record):
                log.debug("Matched notification `{}` with {}".format(notification.name, record))
                if len(notification.action_plugins) > 0:
                    if notification.delay > 0:
                        log.debug("Notification `{}` will be sent in {}s".format(notification.name, notification.delay))
                        self.delay(notification, record)
                    else:
                        log.debug("Notification `{}` will be sent now".format(notification.name))
                        notification.send(record)
                else:
                    log.error("Notification {} has no action. Cannot send".format(self.name))
        return record

    def delay(self, notification, record):
        if not 'hash' in record:
            if 'raw' in record:
                record['hash'] = hashlib.md5(record['raw']).hexdigest()
            else:
                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
        self.thread.delayed[record['hash']] = {'notification': notification, 'record': record, 'time': time.time() + notification.delay}
        self.core.db.write('notification.delay', {'notification_uid': notification.uid, 'record': record, 'host': self.hostname})

    def reload_data(self, sync = False):
        super().reload_data()
        self.notifications = []
        for f in (self.data or []):
            self.notifications.append(NotificationObject(f, self.core))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class NotificationObject():
    def __init__(self, notification, core):
        self.core = core
        self.uid = notification.get('uid')
        self.enabled = notification.get('enabled', True)
        self.name = notification['name']
        self.condition = Condition(notification.get('condition'))
        self.delay = notification.get('delay', 0)
        self.actions = notification.get('actions', [])
        self.action_plugins = []
        if (type(self.actions) is list) and len(self.actions) > 0:
            query = ['IN', self.actions, 'name']
            action_search = self.core.db.search('action', query)
            if action_search['count'] > 0:
                actions_data = action_search['data']
                for action_data in actions_data:
                    action = action_data.get('action', {})
                    content = action.get('subcontent', {})
                    content['action_name'] = action_data.get('name')
                    self.action_plugins.append({'action': self.core.get_action_plugin(action.get('selected')), 'content': content})
            elif self.enabled:
                log.error("Could not find any action defined notification {}. Disabling".format(self.name))
                self.enabled = False
        elif self.enabled:
            log.error("Notification {} has no action. Disabling".format(self.name))
            self.enabled = False
        # Initializing the time constraints
        log.debug("Init Notification filter {} Time Constraints".format(self.name))
        self.time_constraint = init_time_constraints(notification.get('time_constraints', {}))

    def match(self, record):
        '''Whether a record match the Notification object'''
        return self.condition.match(record) and self.time_constraint.match(get_record_date(record))

    def send_delayed(self, record):
        delayed_records = self.core.db.search('record', ['=', 'hash', record.get('hash')])
        if delayed_records['count'] > 0:
            for delayed_record in delayed_records['data']:
                if delayed_record.get('state') not in ['ack', 'close']:
                    self.send(record)
                else:
                    log.debug("record {} is already acked or closed, do not notify".format(record.get('hash')))
        self.core.db.delete('notification.delay', ['=', 'record.hash', record.get('hash')])

    def send(self, record):
        for action_plugin in self.action_plugins:
            if not 'notifications' in record:
                record['notifications'] = []
                record['notifications'].append(self.name)
            action = action_plugin.get('action')
            action_content = action_plugin.get('content', {})
            action_name = action_content.get('action_name', '')
            try:
                log.debug('Action {}'.format(action_plugin))
                action.send(record, action_content)
                self.core.stats.inc('notification_sent', {'name': self.name, 'action': action_name})
            except Exception as e:
                self.core.stats.inc('notification_error', {'name': self.name, 'action': action_name})
                log.error("Notification {} action '{}' could not be send".format(self.name, action_name))
                log.exception(e)

class NotificationThread(threading.Thread):

    def __init__(self, notification):
        super().__init__()
        self.notification = notification
        self.main_thread = threading.main_thread()
        self.delayed = {}

    def run(self):
        while True:
            if not self.main_thread.is_alive():
                break
            for rec_hash in list(self.delayed.keys()):
                if time.time() >= self.delayed[rec_hash]['time']:
                    self.delayed[rec_hash]['notification'].send_delayed(self.delayed[rec_hash]['record'])
                    try:
                        del self.delayed[rec_hash]
                    except KeyError:
                        continue
            time.sleep(2)
