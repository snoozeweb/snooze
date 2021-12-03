#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

import logging
import time
import datetime
import threading

from logging import getLogger
from snooze.utils import config
log = getLogger('snooze.housekeeping')

class Housekeeper():
    def __init__(self, core):
        log.debug('Init Housekeeper')
        self.core = core
        self.conf = None
        self.thread = None
        self.interval_record = None
        self.interval_comment = None
        self.snooze_expired = None
        self.notification_expired = None
        self.reload()
        self.thread = HousekeeperThread(self)
        self.thread.start()

    def reload(self):
        self.conf = config('housekeeping')
        self.interval_record = self.conf.get('cleanup_alert', 300)
        self.interval_comment = self.conf.get('cleanup_comment', 86400)
        self.snooze_expired = self.conf.get('cleanup_snooze', 259200)
        self.notification_expired = self.conf.get('cleanup_notification', 259200)
        log.debug("Reloading Housekeeper with conf {}".format(self.conf))

class HousekeeperThread(threading.Thread):

    def __init__(self, housekeeper):
        super().__init__()
        self.housekeeper = housekeeper
        self.main_thread = threading.main_thread()

    def run(self):
        timer_record = (1 - self.housekeeper.conf.get('trigger_on_startup', True)) * time.time()
        timer_comment = timer_record
        last_day = -1
        while True:
            if not self.main_thread.is_alive():
                break
            if self.housekeeper.interval_record > 0 and time.time() - timer_record >= self.housekeeper.interval_record:
                timer_record = time.time()
                self.housekeeper.core.db.cleanup_timeout('record')
            if self.housekeeper.interval_comment > 0 and time.time() - timer_comment >= self.housekeeper.interval_comment:
                timer_comment = time.time()
                self.housekeeper.core.db.cleanup_orphans('comment', 'record_uid', 'record', 'uid')
            day = datetime.datetime.now().day
            if day != last_day:
                last_day = day
                self.cleanup_expired('snooze', self.housekeeper.snooze_expired)
                self.cleanup_expired('notification', self.housekeeper.notification_expired)
            time.sleep(1)

    def cleanup_expired(self, collection, cleanup_delay):
        if cleanup_delay > 0:
            log.debug("Starting to cleanup expired {}".format(collection))
            now = datetime.datetime.now().astimezone()
            date = now.astimezone().strftime("%Y-%m-%dT%H:%M")
            hour = now.astimezone().strftime("%H:%M")
            weekday = now.day
            date_delta = (now - datetime.timedelta(seconds=cleanup_delay)).astimezone().strftime("%Y-%m-%dT%H:%M")
            match = ['AND',
                ['OR', ['NOT', ['EXISTS', 'time_constraints.weekdays']], ['IN', weekday, 'time_constraints.weekdays.weekdays']],
                ['AND',
                    ['OR', ['NOT', ['EXISTS', 'time_constraints.datetime']], ['<=', 'time_constraints.datetime.from', date]],
                    ['AND',
                        ['OR', ['NOT', ['EXISTS', 'time_constraints.datetime']], ['>=', 'time_constraints.datetime.until', date]],
                        ['AND',
                            ['OR', ['NOT', ['EXISTS', 'time_constraints.time']], ['<=', 'time_constraints.time.from', hour]],
                            ['OR', ['NOT', ['EXISTS', 'time_constraints.time']], ['>=', 'time_constraints.time.until', hour]]
                        ]
                    ]
                ]
            ]
            expired_query = ['AND', ['NOT', match], ['AND', ['EXISTS', 'time_constraints.datetime'], ['NOT', ['>=', 'time_constraints.datetime.until', date_delta]]]]
            expired_results = self.housekeeper.core.db.search(collection, expired_query)
            if expired_results['count'] > 0:
                log.debug("List of expired {} to cleanup: {}".format(collection, expired_results))
                deleted_results = self.housekeeper.core.db.delete(collection, expired_query)
                log.debug("Deleted {} {}".format(deleted_results['count'], collection))
