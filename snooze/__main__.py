#!/usr/bin/python3.6

from snooze.core import Core
from snooze.api.base import Api
from snooze.utils import config
from logging import getLogger
import logging.config
import yaml
import os

def setup_logging(conf):
    logging_config = config('logging')
    if os.environ.get('SNOOZE_DEBUG', conf.get('debug', False)):
        logging_config['handlers']['console']['level'] = 'DEBUG'
        logging_config['loggers']['snooze']['level'] = 'DEBUG'
    logging.config.dictConfig(logging_config)
    log = getLogger('snooze')
    log.debug("Log system on")

def app(conf={}):
    conf.update(config())
    setup_logging(conf)
    core = Core(conf)

    api = Api(core, False)
    return api.handler

def main(conf={}):
    conf.update(config())
    setup_logging(conf)
    core = Core(conf)

    api = Api(core)
    api.serve()

if __name__ == '__main__':
    main()
