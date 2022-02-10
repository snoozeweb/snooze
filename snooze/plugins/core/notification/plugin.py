#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6


import time
import threading
import hashlib
import socket
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.time_constraints import get_record_date, init_time_constraints

from logging import getLogger
log = getLogger('snooze.notification')

from snooze.plugins.core import Plugin

class Notification(Plugin):
    def process(self, record):
        log.debug("Processing record {} against notifications".format(str(record.get('hash', ''))))
        for notification in self.notifications:
            if notification.enabled and notification.match(record):
                log.debug("Notification {} matched record: {}".format(str(notification.name), str(record.get('hash', ''))))
                if len(notification.action_plugins) > 0:
                    actions_success = []
                    total = notification.freq.get('total', 1)
                    was_sent = None
                    if notification.freq.get('delay', 0) <= 0 and total != 0:
                        log.debug("{} Notification(s) `{}` will be sent now".format(total, notification.name))
                        was_sent = notification.send(record, actions_success)
                    every = notification.freq.get('every', 0)
                    delay = max(notification.freq.get('delay', 0), 0) or every
                    retry = self.get_default(record, 'notification_retry', 3)
                    if was_sent == False:
                        every = self.get_default(record, 'notification_freq', 60)
                        delay = every
                        if retry == 0:
                            log.debug("Notification `{}` has 0 retry configured, discarding...".format(notification.name))
                            delay = 0
                    if delay > 0 and (total < 0 or total > (1 if was_sent else 0)):
                        if not 'hash' in record:
                            if 'raw' in record:
                                record['hash'] = hashlib.md5(record['raw']).hexdigest()
                            else:
                                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
                        self.delay_send(notification, record['hash'], delay, every, total - 1, self.get_default(record, 'notification_retry', 3), was_sent, actions_success)
                else:
                    log.error("Notification {} has no action. Cannot send".format(self.name))
        return record

    def get_default(self, record, key, default_val):
        val = record.get(key)
        if val:
            return val
        else:
            return self.core.notif_conf.get(key, default_val)

    def validate(self, obj):
        '''Validate a notification object'''
        validate_condition(obj)

    def delay_send(self, notification, record_hash, delay, every, total, retry, was_sent, actions_success):
        log.debug("Notification `{}` will be {}sent in {}s ({} retries left)".format(notification.name, '' if was_sent == True else 're', delay, retry))
        if was_sent == False:
            total += 1
        self.thread.set_delayed(record_hash, notification.uid, {'notification': notification, 'time': time.time() + delay, 'every': every, 'total': total, 'retry': retry, 'actions_success': actions_success})
        self.core.db.write('notification.delay', {'notification_uid': notification.uid, 'record_hash': record_hash, 'host': self.hostname, 'every': every, 'delay': delay, 'total': total, 'retry': retry, 'actions_success': actions_success}, 'notification_uid,record_hash')

    def post_init(self):
        super().post_init()
        self.hostname = socket.gethostname()
        self.thread = NotificationThread(self)
        self.thread.start()
        delayed_notifs = self.core.db.search('notification.delay', ['=', 'host', self.hostname])
        if delayed_notifs['count'] > 0:
            for delayed_notif in delayed_notifs['data']:
                notif_uid = delayed_notif['notification_uid']
                queue_it = [notif for notif in self.notifications if notif.uid == notif_uid]
                if len(queue_it) > 0:
                    notification = queue_it[0]
                    record_hash = delayed_notif['record_hash']
                    delay = delayed_notif['delay']
                    every = delayed_notif['every']
                    total = delayed_notif['total']
                    retry = delayed_notif['retry']
                    actions_success = delayed_notif['actions_success']
                    self.thread.set_delayed(record_hash, notif_uid, {'notification': notification, 'time': time.time() + delay, 'every': every, 'total': total, 'retry': retry, 'actions_success': actions_success})
                else:
                    log.debug("Delayed notification {} original notification in not in the database anymore. Removing it from queue".format(delayed_notif))
                    self.core.db.delete('notification.delay', ['=', 'uid', delayed_notif['uid']])
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
        self.condition = get_condition(notification.get('condition'))
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

    def send_delayed(self, record_hash, actions_success):
        delayed_records = self.core.db.search('record', ['=', 'hash', record_hash])
        if delayed_records['count'] > 0:
            for delayed_record in delayed_records['data']:
                if delayed_record.get('state') not in ['ack', 'close']:
                    was_sent = self.send(delayed_record, actions_success)
                    self.core.db.write('record', delayed_record)
                    return (True, was_sent)
                else:
                    log.debug("Record {} is already acked or closed, do not notify".format(record_hash))
                    return (False, True)
        else:
            log.debug("Record {} does not exist anymore, do not notify".format(record_hash))
            return (False, True)

    def send(self, record, actions_success):
        if not 'notifications' in record:
            record['notifications'] = []
        if self.name not in record['notifications']:
            record['notifications'].append(self.name)
        was_sent = True
        for action_plugin in self.action_plugins:
            action = action_plugin.get('action')
            action_content = action_plugin.get('content', {})
            action_name = action_content.get('action_name', '')
            if action_name not in actions_success:
                sent = False
                try:
                    log.debug('Action {}'.format(action_plugin))
                    action.send(record, action_content)
                    actions_success.append(action_name)
                    sent = True
                except Exception as e:
                    log.exception(e)
                    log.error("Notification {} action {}' could not be send".format(self.name, action_name))
                    sent = False
                    was_sent = False
                try:
                    if sent:
                        self.core.stats.inc('notification_sent', {'name': self.name, 'action': action_name})
                    else:
                        self.core.stats.inc('notification_error', {'name': self.name, 'action': action_name})
                except Exception as e:
                    log.exception(e)
        return was_sent


class NotificationThread(threading.Thread):

    def __init__(self, notification):
        super().__init__()
        self.notification = notification
        self.main_thread = threading.main_thread()
        self.delayed = {}

    def set_delayed(self, record_hash, notif_uid, val):
        if record_hash not in self.delayed:
            self.delayed[record_hash] = {}
        self.delayed[record_hash][notif_uid] = val

    def run(self):
        while True:
            if not self.main_thread.is_alive():
                break
            for record_hash in list(self.delayed.keys()):
                for notif_uid in list(self.delayed[record_hash].keys()):
                    if time.time() >= self.delayed[record_hash][notif_uid]['time']:
                        notification = self.delayed[record_hash][notif_uid]['notification']
                        actions_success = self.delayed[record_hash][notif_uid]['actions_success']
                        keep, was_sent = notification.send_delayed(record_hash, actions_success)
                        every = max(self.delayed[record_hash][notif_uid]['every'], 0)
                        total = max(self.delayed[record_hash][notif_uid]['total'], -1)
                        retry = self.delayed[record_hash][notif_uid]['retry'] - 1
                        if (was_sent or retry > 0) and keep and every >= 0 and total != 0:
                            self.notification.delay_send(notification, record_hash, every, every, total - 1, retry, was_sent, actions_success)
                        else:
                            if keep:
                                self.notification.core.db.delete('notification.delay', ['AND', ['=', 'record_hash', record_hash], ['=', 'notification_uid', notif_uid]])
                                try:
                                    del self.delayed[record_hash][notif_uid]
                                    if not self.delayed[record_hash]:
                                        del self.delayed[record_hash]
                                except KeyError:
                                    continue
                            else:
                                self.notification.core.db.delete('notification.delay', ['=', 'record_hash', record_hash])
                                try:
                                    del self.delayed[record_hash]
                                except KeyError:
                                    break
                                break
            time.sleep(2)
