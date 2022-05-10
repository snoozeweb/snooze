#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Initialize the different threads'''

import logging.config
import os
import sys
from logging import getLogger

import yaml

from snooze.core import Core
from snooze.api import Api
from snooze.utils.config import setup_logging

def exit_all(threads, exit_code=0):
    '''Stop all threads, and exit'''
    for thread in threads:
        if thread.is_alive():
            thread.stop_thread()
    sys.exit(exit_code)

def app():
    '''Used to initialize the application in Docker Heroku'''
    setup_logging()
    core = Core()

    api = Api(core)
    return api.handler

def main():
    '''Main thread when running snooze-server executable'''
    log = setup_logging()
    core = Core()

    try:
        for thread in core.threads.values():
            thread.start()
        if core.exit_event.wait():
            exit_all(core.threads.values())
    except (KeyboardInterrupt, SystemExit):
        exit_all(core.threads.values(), 1)

if __name__ == '__main__':
    main()
