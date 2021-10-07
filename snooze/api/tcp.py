#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A WSGI server binding to a TCP port'''

import os
import socket
import ssl
from logging import getLogger
from threading import Thread, Event
from wsgiref.simple_server import WSGIServer, WSGIRequestHandler

from socketserver import ThreadingMixIn

log = getLogger('snooze.api.tcp')

class NoLogHandler(WSGIRequestHandler):
    '''Handler that doesn't log to stdout'''
    def log_message(self, *args):
        '''Overriding log to avoid stdout logs'''
        pass
    def handle(self):
        '''
        Bug in socketserver. It doesn't catch exceptions.
        https://bugs.python.org/issue14574
        '''
        try:
            WSGIRequestHandler.handle(self)
        except socket.error:
            pass
        except Exception as err:
            log.warning(err)

class WSGITCPServer(ThreadingMixIn, WSGIServer, Thread):
    daemon_threads = True

    def __init__(self, conf, api, exit_button=None):
        self.exit_button = exit_button or Event()
        self.timeout = 10

        host = conf.get('listen_addr', '0.0.0.0')
        port = conf.get('port', '5200')
        self.ssl_conf = conf.get('ssl', {})

        WSGIServer.__init__(self, (host, port), NoLogHandler)
        self.set_app(api)
        self.wrap_ssl()

        Thread.__init__(self)

    def wrap_ssl(self):
        '''Wrap the socket with a TLS socket when TLS is enabled'''
        use_ssl = self.ssl_conf.get('enabled')
        certfile = os.environ.get('SNOOZE_CERT_FILE') or self.ssl_conf.get('certfile')
        keyfile = os.environ.get('SNOOZE_KEY_FILE') or self.ssl_conf.get('keyfile')
        if use_ssl or (certfile and keyfile):
            if not os.access(certfile, os.R_OK):
                log.error("%s is not readable. Cannot start server", certfile)
                return
            if not os.access(keyfile, os.R_OK):
                log.error("%s is not readable. Cannot start server", keyfile)
                return
            self.socket = ssl.wrap_socket(
                self.socket,
                server_side=True,
                certfile=certfile,
                keyfile=keyfile,
            )

    def run(self):
        '''Override Thread method. Start the service'''
        log.debug('Starting REST API')
        try:
            self.serve_forever()
        except Exception as err:
            log.error(err)
            self.stop()
            self.exit_button.set()

    def stop(self):
        '''Gracefully stop the service'''
        log.info('Closing TCP socket...')
        self.shutdown()
        log.debug("Closed TCP listener")
