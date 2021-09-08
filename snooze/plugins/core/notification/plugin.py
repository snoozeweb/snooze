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
    def process(self, record):
        for notification in self.notifications:
            if notification.enabled and notification.match(record):
                log.debug("Matched notification `{}` with {}".format(notification.name, record))
                if len(notification.action_plugins) > 0:
                    total = notification.freq.get('total', 1)
                    if notification.freq.get('delay', 0) <= 0 and total != 0:
                        log.debug("{} Notification(s) `{}` will be sent now".format(total, notification.name))
                        notification.send(record)
                        total -= 1
                    delay = max(notification.freq.get('delay', 0), 0) or notification.freq.get('every', 0)
                    if delay > 0 and total != 0:
                        log.debug("Notification `{}` will be sent in {}s".format(notification.name, delay))
                        if not 'hash' in record:
                            if 'raw' in record:
                                record['hash'] = hashlib.md5(record['raw']).hexdigest()
                            else:
                                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
                        self.delay_send(notification, record['hash'], delay, total - 1)
                else:
                    log.error("Notification {} has no action. Cannot send".format(self.name))
        return record

    def delay_send(self, notification, record_hash, delay, total):
        self.thread.delayed[record_hash] = {'notification': notification, 'time': time.time() + delay, 'total': total}
        self.core.db.write('notification.delay', {'notification_uid': notification.uid, 'record_hash': record_hash, 'host': self.hostname, 'delay': delay, 'total': total}, 'record.hash')

    def post_init(self):
        super().post_init()
        self.hostname = socket.gethostname()
        self.thread = NotificationThread(self)
        self.thread.start()
        delayed_notifs = self.core.db.search('notification.delay', ['=', 'host', self.hostname])
        if delayed_notifs['count'] > 0:
            for delayed_notif in delayed_notifs['data']:
                notif_uid = delayed_notif['notification_uid']
                notification = next(notif for notif in self.notifications if notif.uid == delayed_notif['notification_uid'])
                record_hash = delayed_notif['record_hash']
                delay = delayed_notif['delay']
                total = delayed_notif['total']
                self.thread.delayed[record_hash] = {'notification': notification, 'time': time.time() + delay, 'total': total}
            log.debug("Restored notification queue {}".format(self.thread.delayed))

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
        self.freq = notification.get('frequency', {})
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
                    log.debug("Adding action plugin {}".format(action))
                    self.action_plugins.append({'action': self.core.get_core_plugin(action.get('selected')), 'content': content})
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

    def send_delayed(self, record_hash):
        delayed_records = self.core.db.search('record', ['=', 'hash', record_hash])
        if delayed_records['count'] > 0:
            for delayed_record in delayed_records['data']:
                if delayed_record.get('state') not in ['ack', 'close']:
                    self.send(delayed_record)
                    self.core.db.write('record', delayed_record)
                    return True
                else:
                    log.debug("record {} is already acked or closed, do not notify".format(record.get('hash')))
                    return False

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
                log.error("Notification {} action {}' could not be send".format(self.name, action_name))
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
            for record_hash in list(self.delayed.keys()):
                if time.time() >= self.delayed[record_hash]['time']:
                    notification = self.delayed[record_hash]['notification']
                    can_delete = not notification.send_delayed(record_hash)
                    every = max(notification.freq.get('every', 0), 0)
                    total = max(self.delayed[record_hash]['total'], -1)
                    if not can_delete and every >= 0 and total != 0:
                        self.notification.delay_send(notification, record_hash, every, total - 1)
                    else:
                        self.notification.core.db.delete('notification.delay', ['=', 'record.hash', record_hash])
                        try:
                            del self.delayed[record_hash]
                        except KeyError:
                            continue
            time.sleep(2)
