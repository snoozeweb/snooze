#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Some utils for threading purpose'''

from abc import abstractmethod
from collections import deque
from datetime import datetime, timedelta
from logging import getLogger
from threading import Thread, Event
from typing import Optional, Deque

from tenacity import Retrying, RetryCallState

log = getLogger('snooze.utils.threading')

class RateLimit:
    '''Tenacity retry strategy.
    Maintain a queue of all times we got a hit, and remove the ones that
    exceed the interval.'''
    max_hits: int
    interval: timedelta
    failures: Deque[datetime]

    def __init__(self, name, max_hits: int, interval: timedelta):
        self.name = name
        self.max_hits = max_hits
        self.interval = interval
        self.failures: Deque[datetime] = deque()

    def __call__(self, retry_state: RetryCallState):
        if not retry_state.outcome.failed:
            return False

        now = datetime.now()
        self.failures.append(now)

        # Remove elements in queue not fitting the interval
        while len(self.failures) and (now - self.failures[0]) >= self.interval:
            self.failures.popleft()

        log.error("Retrying thread %s. Failed %d times in interval %s", self.name, len(self.failures), self.interval,
            exc_info=retry_state.outcome.exception())
        if len(self.failures) > self.max_hits:
            return False

        return True

class SurvivingThread(Thread):
    '''A thread that catch exceptions and can restart itself or signal its death through an event'''
    def __init__(self, exit_event: Optional[Event] = None, critical: bool = False):
        self.exit = exit_event or Event()
        self.critical = critical
        Thread.__init__(self)

    def run(self):
        try:
            for attempt in Retrying(retry=RateLimit(self.__class__.__name__, 3, timedelta(minutes=1)), reraise=True):
                with attempt:
                    self.start_thread()
                    self.exit.set()
                    break
        except Exception as err:
            log.exception(err)
            self.stop_thread()
            if self.critical:
                # Trigger all other thread to stop
                self.exit.set()

    @abstractmethod
    def start_thread(self):
        '''A wrapper to start the thread'''

    def stop_thread(self):
        '''A wrapper to stop the thread and cleanup'''
