#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module with the database housekeeping functionalities'''

from time import sleep
from abc import abstractmethod, ABC
from datetime import datetime, timedelta, time
from logging import getLogger
from threading import Event
from typing import Callable, Optional
from pathlib import Path

from snooze.utils.config import HousekeeperConfig, BackupConfig
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.housekeeping')

class AbstractJob(ABC):
    '''An abstract class for a job'''
    def __init__(self, *_args, **_kwargs):
        self.next_run = datetime.now()

    @abstractmethod
    def run(self, db: 'Database'):
        '''The method that will run when the job is triggered'''

    @abstractmethod
    def next(self):
        '''Will move the timer to the next step, computing next_run'''

    def reload(self, conf: dict):
        '''A method to reload the configuration (if any)'''

# TODO Support negative intervals with a enabled/disabled system
class BasicJob(AbstractJob):
    '''A housekeeping job triggerring a db based callback at a regular interval'''
    def __init__(self,
        config_key: str,
        interval: timedelta,
        callback: Callable[['Database', int], None],
    ):
        AbstractJob.__init__(self)
        self.config_key = config_key
        self.interval = interval
        self.callback = callback

    def run(self, db):
        self.callback(db)

    def next(self):
        self.next_run += self.interval

    def reload(self, conf: dict):
        '''Called to reload the configuration of the job'''
        new_interval = getattr(conf, self.config_key)
        if new_interval:
            self.interval = new_interval

class CronJob(AbstractJob):
    '''Run everyday at a given time'''
    def __init__(self,
        config_key: str,
        cron_time: time,
        cleanup_interval: Optional[timedelta],
        callback: Callable[['Database', int], None],
    ):
        AbstractJob.__init__(self)
        self.cleanup_interval = cleanup_interval
        self.callback = callback
        self.cron_time = cron_time

    def run(self, db):
        self.callback(db, self.cleanup_interval)

    def next(self):
        now = datetime.now()
        next_run = datetime.combine(now.date(), self.cron_time)
        if now < next_run:
            self.next_run = next_run
        else:
            self.next_run = next_run + timedelta(days=1)

# TODO Support negative intervals with a enabled/disabled system
class IntervalJob(AbstractJob):
    '''A housekeeping job triggerring a db based callback at a regular interval,
    with another interval passed to the db callback'''
    def __init__(self,
        config_key: str,
        run_interval: timedelta,
        cleanup_interval: timedelta,
        callback: Callable[['Database', int], None],
    ):
        AbstractJob.__init__(self)
        self.config_key = config_key
        self.run_interval = run_interval
        self.cleanup_interval = cleanup_interval
        self.callback = callback

    def run(self, db):
        self.callback(db, self.cleanup_interval)

    def next(self):
        self.next_run += self.run_interval

    def reload(self, conf: dict):
        '''Called to reload the configuration of the job'''
        new_interval = getattr(conf, self.config_key)
        if new_interval:
            self.cleanup_interval = new_interval

class BackupJob(AbstractJob):
    '''A dedicated class for the backup job'''
    def __init__(self, config: BackupConfig, interval: timedelta):
        self.config = config
        self.interval = interval
        try:
            self.config.path.mkdir(parents=True, exist_ok=True)
        except OSError:
            self.config.path = Path('.')
        self.reload({})
        AbstractJob.__init__(self)

    def next(self):
        self.next_run += self.interval

    def run(self, db):
        db.backup(self.config.path, self.config.excludes)

    def reload(self, conf: dict):
        ...

class Housekeeper(SurvivingThread):
    '''Main class starting the housekeeping thread in the background'''
    def __init__(self,
        config: HousekeeperConfig,
        backup: BackupConfig,
        db: 'Database',
        exit_event: Optional[Event] = None,
    ):
        log.debug('Init Housekeeper')
        self.config = config
        self.db = db
        self.backup = backup
        self.jobs = {
            'cleanup_alert': BasicJob('cleanup_alert', timedelta(minutes=5),
                lambda db: db.cleanup_timeout('record')),
            'cleanup_aggregate': BasicJob('cleanup_aggregate', timedelta(minutes=1),
                lambda db: db.drop('aggregate')),
            'cleanup_comment': BasicJob('cleanup_comment', timedelta(days=1),
                lambda db: db.cleanup_comments()),
            'cleanup_orphans': BasicJob('cleanup_orphans', timedelta(days=1),
                lambda db: db.cleanup_orphans('rule')),
            'renumber_field': CronJob('renumber_field', time(hour=0, minute=0), None,
                lambda db, _: db.renumber_field('rule', 'tree_order')),
            'cleanup_audit': CronJob('cleanup_audit', time(hour=0, minute=0), timedelta(days=28),
                lambda db, interval: db.cleanup_audit_logs(interval.total_seconds())),
            'cleanup_snooze': CronJob('cleanup_snooze', time(hour=0, minute=0), timedelta(days=3),
                lambda db, interval: cleanup_expired(db, 'snooze', interval.total_seconds())),
            'cleanup_notification': CronJob('cleanup_notification', time(hour=0, minute=0), timedelta(days=3),
                lambda db, interval: cleanup_expired(db, 'notification', interval.total_seconds())),
        }
        self.backup_job = None
        self.reload()
        if self.config.trigger_on_startup:
            for job in self.jobs.values():
                job.next_run = datetime.now()
        SurvivingThread.__init__(self, exit_event)

    def reload(self):
        ''' Reload the housekeeper configuration'''
        self.config.refresh()
        log.debug("Reloading Housekeeper with config %s", self.config)
        for job in self.jobs.values():
            job.reload(self.config)

        # Backup config
        if self.backup.enabled:
            if not self.backup_job:
                self.backup_job = BackupJob(self.backup, timedelta(days=1))
            self.backup_job.reload(self.backup)
        else:
            self.backup_job = None

    def handler(self):
        '''The check to execute at every loop'''
        for name, job in self.jobs.items():
            if job.next_run <= datetime.now():
                job.next()
                log.debug("Job %s starting...", name)
                job.run(self.db)
                log.debug("Job %s done. Next run: %s", name, job.next_run)
        if self.backup_job:
            if self.backup_job.next_run <= datetime.now():
                log.debug("Job backup starting...")
                self.backup_job.run(self.db)
                self.backup_job.next()
                log.debug("Job backup done. Next run: %s", self.backup_job.next_run)

    def start_thread(self):
        if self.config.trigger_on_startup:
            for _, job in self.jobs.items():
                job.next_run = datetime.now()
        while not self.exit.wait(0.1):
            self.handler()
            sleep(1)
        log.info('Stopped housekeeper')

def cleanup_expired(db: 'Database', collection: str, cleanup_delay: int):
    '''Cleanup expired objects. Used for objects containing a time constraint, and
    that have an expiration date, like snooze filters'''
    if cleanup_delay > 0:
        log.debug("Starting to cleanup expired %s", collection)
        now = datetime.now().astimezone()
        date = now.strftime("%Y-%m-%dT%H:%M")
        hour = now.strftime("%H:%M")
        weekday = now.day
        date_delta = (now - timedelta(seconds=cleanup_delay)).astimezone().strftime("%Y-%m-%dT%H:%M")
        match = ['AND',
            ['OR',
                ['NOT', ['EXISTS', 'time_constraints.weekdays']],
                ['IN', weekday, 'time_constraints.weekdays.weekdays'],
            ],
            ['AND',
                ['OR',
                    ['NOT', ['EXISTS', 'time_constraints.datetime']],
                    ['<=', 'time_constraints.datetime.from', date],
                ],
                ['AND',
                    ['OR',
                        ['NOT', ['EXISTS', 'time_constraints.datetime']],
                        ['>=', 'time_constraints.datetime.until', date],
                    ],
                    ['AND',
                        ['OR',
                            ['NOT', ['EXISTS', 'time_constraints.time']],
                            ['<=', 'time_constraints.time.from', hour],
                        ],
                        ['OR',
                            ['NOT', ['EXISTS', 'time_constraints.time']],
                            ['>=', 'time_constraints.time.until', hour],
                        ],
                    ]
                ]
            ]
        ]
        expired_query = ['AND',
            ['NOT', match],
            ['AND',
                ['EXISTS', 'time_constraints.datetime'],
                ['NOT', ['>=', 'time_constraints.datetime.until', date_delta]],
            ],
        ]
        expired_results = db.search(collection, expired_query)
        if expired_results['count'] > 0:
            log.debug("List of expired %s to cleanup: %s", collection, expired_results)
            deleted_results = db.delete(collection, expired_query)
            log.debug("Deleted %s %s", deleted_results['count'], collection)
