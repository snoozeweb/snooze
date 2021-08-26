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
        self.interval = None
        self.snooze_expired = None
        self.reload()
        self.thread = HousekeeperThread(self)
        self.thread.start()

    def reload(self):
        self.conf = config('housekeeping')
        self.interval = self.conf.get('cleanup_interval', 60)
        self.snooze_expired = self.conf.get('cleanup_snooze', 86400)
        log.debug("Reloading Housekeeper with conf {}".format(self.conf))

class HousekeeperThread(threading.Thread):

    def __init__(self, housekeeper):
        super().__init__()
        self.housekeeper = housekeeper
        self.main_thread = threading.main_thread()

    def run(self):
        timer = (1 - self.housekeeper.conf.get('trigger_on_startup', True)) * time.time()
        last_day = -1
        while True:
            if not self.main_thread.is_alive():
                break
            if self.housekeeper.interval > 0 and time.time() - timer >= self.housekeeper.interval:
                timer = time.time()
                self.housekeeper.core.db.cleanup_timeout('record')
                self.housekeeper.core.db.cleanup_orphans('comment', 'record_uid', 'record', 'uid')
            day = datetime.datetime.now().day
            if day != last_day:
                last_day = day
                self.cleanup_expired_snooze()
            time.sleep(1)

    def cleanup_expired_snooze(self):
        if self.housekeeper.snooze_expired > 0:
            log.debug("Starting to cleanup expired snooze filters")
            now = datetime.datetime.now().astimezone()
            date = now.astimezone().strftime("%Y-%m-%dT%H:%M")
            hour = now.astimezone().strftime("%H:%M")
            weekday = now.day
            date_delta = (now - datetime.timedelta(seconds=self.housekeeper.snooze_expired)).astimezone().strftime("%Y-%m-%dT%H:%M")
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
            expired_results = self.housekeeper.core.db.search('snooze', expired_query)
            if expired_results['count'] > 0:
                log.debug("List of expired snooze filters to cleanup: {}".format(expired_results))
                deleted_results = self.housekeeper.core.db.delete('snooze', expired_query)
                log.debug("Deleted {} snooze filters".format(deleted_results['count']))
