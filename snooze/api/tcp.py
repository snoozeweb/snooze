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
from threading import Event
from typing import Optional, Tuple
from wsgiref.simple_server import WSGIServer, WSGIRequestHandler

from socketserver import ThreadingMixIn

from snooze.utils.config import SslConfig
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.api.tcp')

class NoLogHandler(WSGIRequestHandler):
    '''Handler that doesn't log to stdout'''
    def log_message(self, *args):
        '''Overriding log to avoid stdout logs'''

    def handle(self):
        '''Bug in socketserver. It doesn't catch exceptions.
        https://bugs.python.org/issue14574
        '''
        try:
            WSGIRequestHandler.handle(self)
        except socket.error:
            pass
        except Exception as err:
            log.warning(err)

class TcpWsgiServer(ThreadingMixIn, WSGIServer):
    '''Multi threaded TCP server serving a WSGI application'''
    daemon_threads = True

    def __init__(self, host: str, port: int, sslconf: SslConfig, api: 'Api'):
        self.timeout = 10

        self.ssl = sslconf
        WSGIServer.__init__(self, (host, port), NoLogHandler)
        self.set_app(api)
        self.wrap_ssl()

    def wrap_ssl(self):
        '''Wrap the socket with a TLS socket when TLS is enabled'''
        if self.ssl.enabled or (self.ssl.certfile and self.ssl.keyfile):
            if not os.access(self.ssl.certfile, os.R_OK):
                log.error("%s is not readable. Cannot start server", self.ssl.certfile)
                return
            if not os.access(self.ssl.keyfile, os.R_OK):
                log.error("%s is not readable. Cannot start server", self.ssl.keyfile)
                return
            self.socket = ssl.wrap_socket(
                self.socket,
                server_side=True,
                certfile=self.ssl.certfile,
                keyfile=self.ssl.keyfile,
            )


class TcpThread(SurvivingThread):
    '''A TCP thread to manage the multi-threaded TCP server.'''
    daemon_threads = True

    def __init__(self, tcp_config: Tuple[str, int, SslConfig], api: 'Api', exit_event: Optional[Event] = None):
        exit_event = exit_event or Event()
        self.timeout = 10

        self.tcp_config = tcp_config
        self.api = api

        self.server: Optional[TcpWsgiServer] = None
        SurvivingThread.__init__(self, exit_event)

    def start_thread(self):
        '''Override Thread method. Start the service'''
        log.debug('Starting REST API')
        # The WSGIServer is binding on init. This is very inconvenient
        # for many use-cases (testing, etc). So we're manking this thread
        # cheap to initialize.
            self.server = TcpWsgiServer(*self.tcp_config, self.api)
            self.server.serve_forever()

    def stop_thread(self):
        '''Gracefully stop the service'''
        if self.server:
            log.info('Stopping TCP socket...')
            self.server.shutdown()
            log.info("Stopped TCP listener")
