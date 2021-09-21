'''Initialize the different threads'''

import logging.config
import os
import sys

from logging import getLogger

from snooze.api.base import Api
from snooze.api.socket import WSGISocketServer, admin_api
from snooze.api.tcp import WSGITCPServer
from snooze.core import Core
from snooze.utils import config

def setup_logging(conf):
    logging_config = config('logging')
    if os.environ.get('SNOOZE_DEBUG', conf.get('debug', False)):
        logging_config['handlers']['console']['level'] = 'DEBUG'
        logging_config['loggers']['snooze']['level'] = 'DEBUG'
    logging.config.dictConfig(logging_config)
    log = getLogger('snooze')
    log.debug("Log system on")
    return log

def exit_all(threads, exit_code=0):
    '''Stop all threads, and exit'''
    for thread in threads:
        if thread.is_alive():
            thread.stop()
    sys.exit(exit_code)

def app(conf={}):
    conf.update(config())
    setup_logging(conf)
    core = Core(conf)

    api = Api(core)
    return api.handler

def main(conf={}):
    conf.update(config())
    log = setup_logging(conf)
    core = Core(conf)

    threads = []

    api = Api(core)
    tcp_server = WSGITCPServer(core.conf, api.handler, core.exit_button)
    threads.append(tcp_server)

    unix_socket = core.conf.get('unix_socket')
    if unix_socket:
        try:
            socket_server = WSGISocketServer(
                admin_api(core.token_engine),
                unix_socket,
                core.exit_button
            )
            threads.append(socket_server)
        except Exception as err:
            log.warning("Error starting unix socket at %s: %s", unix_socket, err)

    try:
        for thread in threads:
            thread.start()
        if core.exit_button.wait():
            exit_all(threads)
    except (KeyboardInterrupt, SystemExit):
        exit_all(threads, 1)

if __name__ == '__main__':
    main()
