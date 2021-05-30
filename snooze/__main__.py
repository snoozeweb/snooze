#!/usr/bin/python3.6

from snooze.core import Core
from snooze.api.base import Api
from snooze.utils import config
from logging import getLogger
import logging.config
import yaml
import os

from prometheus_client import start_http_server

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

    # Prometheus
    prometheus_port = conf.get('prometheus_port', 9234)
    start_http_server(prometheus_port)

    api = Api(core)
    api.serve()

if __name__ == '__main__':
    main()
