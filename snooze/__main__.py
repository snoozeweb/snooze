#!/usr/bin/python3.6

from snooze.core import Core
from snooze.api.base import Api
from snooze.utils import config
from logging import getLogger
import logging.config
import yaml
import os

def setup_logging(path='logging.yaml'):
    if os.path.exists(path):
        with open(path, 'rt') as f:
            config = yaml.safe_load(f.read())
        logging.config.dictConfig(config)
    log = getLogger('snooze')
    log.debug("Log system on")

def main(conf={}):
    setup_logging()
    conf.update(config())
    core = Core(conf)
    api = Api(core)
    api.serve()

if __name__ == '__main__':
    main()
