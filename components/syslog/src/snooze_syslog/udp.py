"""UDP handler"""

from logging import getLogger
from threading import Thread
from socketserver import DatagramRequestHandler, UDPServer
from queue import Queue

from snooze_syslog.types import LogEntry

LOG = getLogger("snooze.syslog.udp")


class QueuedUDPServer(UDPServer):
    queue: "Queue[LogEntry]"

    def __init__(self, addr, handler, queue):
        super().__init__(addr, handler)
        self.queue = queue


class UDPHandler(DatagramRequestHandler):
    """Handler for UDPServer"""

    def __init__(self, request, client_address, server: QueuedUDPServer):
        self.queue = server.queue
        DatagramRequestHandler.__init__(self, request, client_address, server)

    def handle(self):
        client_addr = self.client_address[0].encode().decode()
        for line in self.rfile:
            LOG.debug("Received from %s: %s", client_addr, line)
            self.queue.put((client_addr, line))


class UDPListener(Thread):
    """Wrap the UDP server into a stoppable process"""

    def __init__(self, host, port, queue: "Queue[LogEntry]"):
        self.server = QueuedUDPServer((host, port), UDPHandler, queue)
        Thread.__init__(self)

    def run(self):
        """Start the UDP listener"""
        LOG.info("Starting UDP listener")
        self.server.serve_forever()

    def stop(self):
        """Stop the UDP listener"""
        LOG.info("Stopping UDP listener")
        self.server.shutdown()
