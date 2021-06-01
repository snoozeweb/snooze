#!/usr/bin/python3.6

import logging
import time

from threading import Thread
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
        self.reload()
        self.thread = HousekeeperThread(self)
        self.thread.start()

    def reload(self):
        self.conf = config('housekeeping')
        self.interval = self.conf.get('cleanup_interval', 60)
        log.debug("Reloading Housekeeper with conf {}".format(self.conf))
        return True

class HousekeeperThread(Thread):

    def __init__(self, housekeeper):
        super().__init__()
        self.housekeeper = housekeeper

    def run(self):
        timer = (1 - self.housekeeper.conf.get('trigger_on_startup', True)) * time.time()
        while True:
            if time.time() - timer >= self.housekeeper.interval:
                timer = time.time()
                self.housekeeper.core.db.cleanup('aggregate')
                self.housekeeper.core.db.cleanup('record')
            time.sleep(1)
