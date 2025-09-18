"""TCP multi-threaded server"""

import ssl
from logging import getLogger
from threading import Thread
from socketserver import TCPServer, ThreadingMixIn, StreamRequestHandler
from queue import Queue

from snooze_syslog.types import LogEntry

LOG = getLogger("snooze.syslog.tcp")


class ThreadedTCPServer(ThreadingMixIn, TCPServer, object):
    """Multi-threaded TCPServer"""

    def __init__(self, queue: "Queue[LogEntry]", config, address, requestHandlerClass):
        self.queue = queue
        self.config = config
        TCPServer.__init__(self, address, requestHandlerClass, bind_and_activate=True)

    def get_request(self):
        if self.config.get("ssl"):
            (socket, addr) = TCPServer.get_request(self)
            ssl_socket = ssl.wrap_socket(
                socket,
                server_side=True,
                certfile=self.config.get("certfile"),
                keyfile=self.config.get("keyfile"),
            )
            return (ssl_socket, addr)
        else:
            return TCPServer.get_request(self)

    def finish_request(self, request, client_address):
        self.RequestHandlerClass(request, client_address, self)

    def server_close(self):
        self.socket.close()
        self.shutdown()
        return TCPServer.server_close(self)


class QueuedTCPRequestHandler(StreamRequestHandler):
    """Handler for TCPServer"""

    def __init__(self, request, client_address, server: ThreadedTCPServer):
        self.queue = server.queue
        StreamRequestHandler.__init__(self, request, client_address, server)

    def handle(self):
        client_addr = self.client_address[0].encode().decode()
        for line in self.rfile:
            LOG.debug("Received from %s: %s", client_addr, line)
            self.queue.put((client_addr, line))

    def finish(self):
        self.request.close()


class TCPListener(Thread):
    """Process wrapping the TCP server into a stoppable process"""

    def __init__(self, host, port, queue, config):
        self.server = ThreadedTCPServer(
            queue,
            config,
            (host, port),
            QueuedTCPRequestHandler,
        )
        Thread.__init__(self)

    def run(self):
        """Start the TCP listener"""
        LOG.info("Starting TCP listener")
        self.server.serve_forever()

    def stop(self):
        """Stop the TCP listener"""
        LOG.info("Stopping TCP listener")
        self.server.shutdown()
