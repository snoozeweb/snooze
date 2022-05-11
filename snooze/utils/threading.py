#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Some utils for threading purpose'''

from abc import abstractmethod
from logging import getLogger
from threading import Thread, Event
from typing import Optional

log = getLogger('snooze.utils.threading')

class SurvivingThread(Thread):
    '''A thread that catch exceptions and can restart itself or signal its death through an event'''
    def __init__(self, exit_event: Optional[Event] = None, restart: bool = False):
        self.exit = exit_event or Event()
        self.restart = restart
        Thread.__init__(self)

    def run(self):
        while True:
            try:
                self.start_thread()
                self.exit.set()
                break
            except Exception as err:
                log.exception(err)
                self.stop_thread()
                if self.restart:
                    continue
                else:
                    self.exit.set()
                    break

    @abstractmethod
    def start_thread(self):
        '''A wrapper to start the thread'''

    def stop_thread(self):
        '''A wrapper to stop the thread and cleanup'''
