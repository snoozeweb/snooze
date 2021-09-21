'''A WSGI server binding to a TCP port'''

import os
import ssl
from logging import getLogger
from threading import Thread, Event

from waitress.adjustments import Adjustments
from waitress.server import TcpWSGIServer

log = getLogger('snooze.wsgi.tcp')

class WSGITCPServer(TcpWSGIServer, Thread):
    '''A TCP server that serve a WSGI application'''
    def __init__(self, conf, api, exit_button=None):
        '''
        Args:
            conf (dict): Configuration of snooze
            api: A WSGI API object
            exit_button (threading.Event): An event used to exit everything if this thread dies
        '''
        self.exit_button = exit_button or Event()
        self.timeout = 10

        self.listen_addr = conf.get('listen_addr', '0.0.0.0')
        self.port = conf.get('port', '5200')
        self.ssl_conf = conf.get('ssl', dict)

        wsgi_options = Adjustments(host=self.listen_addr, port=self.port)
        TcpWSGIServer.__init__(self, api, adj=wsgi_options)
        Thread.__init__(self)

    def handle_connect(self):
        '''Override asyncore dispatcher to provide TLS'''
        use_ssl = self.ssl_conf.get('enabled', False)
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
        log.info("Listening on %s:%s", self.listen_addr, self.port)
        TcpWSGIServer.run(self)

    def excepthook(self, exc_type, exc_value, _exc_traceback, _thread):
        '''Override Thread method. Handle exceptions and gracefully stop'''
        log.error("Fatal: Received error %s: %s", exc_type, exc_value)
        self.stop()
        self.exit_button.set()

    def stop(self):
        '''Gracefully stop the service'''
        log.debug("Closing socket at %s:%s", self.listen_addr, self.port)
        self.close()
        log.debug("Waiting for socket at %s:%s to close...", self.listen_addr, self.port)
        self.join(self.timeout)
        log.debug("Closed TCP listener")
