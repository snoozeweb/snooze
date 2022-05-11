#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module with the database housekeeping functionalities'''

import datetime
import time
from logging import getLogger

from snooze.utils import config
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.housekeeping')

class Housekeeper(SurvivingThread):
    '''Main class starting the housekeeping thread in the background'''
    def __init__(self, core: 'Core'):
        log.debug('Init Housekeeper')
        self.core = core
        self.conf = None
        self.interval_record = None
        self.interval_comment = None
        self.interval_audit = None
        self.snooze_expired = None
        self.notification_expired = None
        self.reload()
        SurvivingThread.__init__(self, core.exit_event)

    def reload(self):
        ''' Reload the housekeeper configuration'''
        self.conf = config('housekeeping')
        self.interval_record = self.conf.get('cleanup_alert', 300)
        self.interval_comment = self.conf.get('cleanup_comment', 86400)
        self.interval_audit = self.conf.get('cleanup_audit', 2419200)
        self.snooze_expired = self.conf.get('cleanup_snooze', 259200)
        self.notification_expired = self.conf.get('cleanup_notification', 259200)
        log.debug("Reloading Housekeeper with conf %s", self.conf)

    def start_thread(self):
        timer_record = (1 - self.conf.get('trigger_on_startup', True)) * time.time()
        timer_comment = timer_record
        timer_audit = timer_record
        last_day = -1
        while not self.exit.wait(0.1):
            if self.interval_record > 0 and time.time() - timer_record >= self.interval_record:
                timer_record = time.time()
                self.core.db.cleanup_timeout('record')
            if self.interval_comment > 0 and time.time() - timer_comment >= self.interval_comment:
                timer_comment = time.time()
                self.core.db.cleanup_orphans('comment', 'record_uid', 'record', 'uid')
            if self.interval_audit > 0 and time.time() - timer_audit >= self.interval_audit:
                timer_audit = time.time()
                self.core.db.cleanup_audit_logs(self.interval_audit)
            day = datetime.datetime.now().day
            if day != last_day:
                last_day = day
                cleanup_expired(self.core.db, 'snooze', self.snooze_expired)
                cleanup_expired(self.core.db, 'notification', self.notification_expired)
                backup_conf = self.core.conf.get('backup', {})
                if backup_conf.get('enabled', True):
                    self.core.db.backup(backup_conf.get('path', '/var/log/snooze'), backup_conf.get('exclude', ['record', 'stats', 'comment', 'secrets']))
            time.sleep(1)
        log.info('Stopped housekeeper')

def cleanup_expired(db: 'Database', collection: str, cleanup_delay: int):
    '''Cleanup expired objects. Used for objects containing a time constraint, and
    that have an expiration date, like snooze filters'''
    if cleanup_delay > 0:
        log.debug("Starting to cleanup expired %s", collection)
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
        expired_results = db.search(collection, expired_query)
        if expired_results['count'] > 0:
            log.debug("List of expired %s to cleanup: %s", collection, expired_results)
            deleted_results = db.delete(collection, expired_query)
            log.debug("Deleted %s %s", deleted_results['count'], collection)
