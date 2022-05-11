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
from typing import Optional
from wsgiref.simple_server import WSGIServer, WSGIRequestHandler

from socketserver import ThreadingMixIn

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

    def __init__(self, conf: dict, api: 'Api'):
        self.timeout = 10

        host = conf.get('listen_addr', '0.0.0.0')
        port = int(conf.get('port', '5200'))
        self.ssl_conf = conf.get('ssl', {})

        WSGIServer.__init__(self, (host, port), NoLogHandler)
        self.set_app(api)
        self.wrap_ssl()

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


class TcpThread(SurvivingThread):
    '''A TCP thread to manage the multi-threaded TCP server.'''
    daemon_threads = True

    def __init__(self, conf: dict, api: 'Api', exit_event: Optional[Event] = None):
        exit_event = exit_event or Event()
        self.timeout = 10

        self.conf = conf
        self.api = api

        self.server: Optional[TcpWsgiServer] = None
        SurvivingThread.__init__(self, exit_event)

    def start_thread(self):
        '''Override Thread method. Start the service'''
        log.debug('Starting REST API')
        # The WSGIServer is binding on init. This is very inconvenient
        # for many use-cases (testing, etc). So we're manking this thread
        # cheap to initialize.
        self.server = TcpWsgiServer(self.conf, self.api)
        self.server.serve_forever()

    def stop_thread(self):
        '''Gracefully stop the service'''
        if self.server:
            log.info('Stopping TCP socket...')
            self.server.shutdown()
            log.info("Stopped TCP listener")
