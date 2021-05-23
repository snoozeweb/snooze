import logging
import os
import platform
import re
import ssl
import sys
import yaml

from multiprocessing import JoinableQueue, Process
from multiprocessing.pool import ThreadPool as Pool
from socketserver import TCPServer, ThreadingMixIn, StreamRequestHandler, DatagramRequestHandler, UDPServer
from threading import Thread
from pathlib import Path

from snooze_client import Snooze

SYSLOG_FACILITY_NAMES = [
    "kern",
    "user",
    "mail",
    "daemon",
    "auth",
    "syslog",
    "lpr",
    "news",
    "uucp",
    "cron",
    "authpriv",
    "ftp",
    "ntp",
    "audit",
    "alert",
    "clock",
    "local0",
    "local1",
    "local2",
    "local3",
    "local4",
    "local5",
    "local6",
    "local7"
]

SYSLOG_SEVERITY_NAMES = [
    "emerg",
    "alert",
    "crit",
    "err",
    "warning",
    "notice",
    "info",
    "debug"
]

def decode_priority(pri):
    '''Decode the syslog facility and severity from the PRI'''
    facility = pri >> 3
    severity = pri & 7
    return SYSLOG_FACILITY_NAMES[facility], SYSLOG_SEVERITY_NAMES[severity]

LOOP_EVERY = 20  # seconds

LOG = logging.getLogger("snooze.syslog")
logging.basicConfig(format="%(asctime)s - %(name)s: %(levelname)s - %(message)s", level=logging.DEBUG)

def parse_rfc3164(msg):
    '''Parse Syslog RFC 3164 message format'''
    m = re.match(r'<(\d{1,3})>\S{3}\s{1,2}\d?\d \d{2}:\d{2}:\d{2} (\S+)( (\S+):)? (.*)', msg)
    if m:
        record = {
            'syslog_type': 'rfc3164',
            'pri': int(m.group(1)),
            'host': m.group(2),
            'message': m.group(5),
        }
        return record
    else:
        raise Exception("Could not parse RFC 3164 syslog message: %s" % msg)

def parse_rfc5424(msg):
    '''Parse Syslog RFC 5424 message format'''
    m = re.match(r'<(\d+)>1 (\S+) (\S+) (\S+) (\S+) (\S+) (.*)', msg)
    if m:
        record = {
            'syslog_type': 'rfc5424',
            'pri': int(m.group(1)),
            'timestamp': m.group(2),
            'host': m.group(3),
            'process': m.group(4),
            'pid': m.group(5),
            'msgid': m.group(6),
            'message': m.group(7),
        }
        return record
    else:
        raise Exception("Could not parse RFC 5424 syslog message: %s" % msg)

def parse_cisco(msg):
    '''Parse Cisco Syslog message format'''
    m = re.match('<(\d+)>.*(%([A-Z0-9_-]+)):? (.*)', msg)
    if m:
        record = {
            'syslog_type': 'cisco',
            'pri': int(m.group(1)),
            'message': m.group(4)
        }
        try:
            facility, severity, mnemonic = m.group(3).split('-')
        except ValueError as err:
            LOG.error('Could not parse Cisco syslog - %s: %s', err, m.group(3))
            facility = severity = mnemonic = 'na'
        record.update({
            'cisco_facility': facility,
            'cisco_severity': severity,
            'cisco_mnemonic': mnemonic,
        })

        return record
    else:
        raise Exception("Could not parse Cisco syslog message: %s" % msg)

def parse_syslog(ipaddr, data):
    '''Parse a syslog message from the queue'''
    LOG.debug('Parsing syslog message...')
    records = list()

    for msg in data.strip().decode().split('\n'):
        LOG.debug("Found: %s", msg)
        record = dict()

        record['source_ip'] = ipaddr

        if not msg or 'last message repeated' in msg:
            LOG.debug("Skipping message: %s", msg)
            continue

        if re.match(r'<\d+>1', msg):
            record.update(parse_rfc5424(msg))

        elif re.match(r'<(\d{1,3})>\S{3}\s', msg):
            record.update(parse_rfc3164(msg))

        elif re.match(r'<\d+>.*%[A-Z0-9_-]+', msg):
            record.update(parse_cisco(msg))

        else:
            LOG.error("Could not parse message: %s", msg)
            continue

        facility, severity = decode_priority(record['pri'])
        record.update({
            'facility': facility,
            'severity': severity,
        })

        records.append(record)

    return records

class ThreadedTCPServer(ThreadingMixIn, TCPServer, object):
    '''Multi-threaded TCPServer'''
    def __init__(self, queue, config, address, requestHandlerClass):
        self.queue = queue
        self.config = config
        LOG.debug("Starting mutlithreaded TCP receiver")
        TCPServer.__init__(self, address, requestHandlerClass, bind_and_activate=True)

    def get_request(self):
        if self.config.get('ssl'):
            (socket, addr) = TCPServer.get_request(self)
            ssl_socket = ssl.wrap_socket(
                socket,
                server_side=True,
                certfile=self.config.get('certfile'),
                keyfile=self.config.get('keyfile'),
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

class UDPHandler(DatagramRequestHandler):
    '''Handler for UDPServer'''
    def handle(self):
        queue = self.server.queue
        client_addr = self.client_address[0].encode().decode()
        for line in self.rfile:
            LOG.debug(f"[udp] Received from {client_addr}: {line}")
            queue.put((client_addr, line))

class QueuedTCPRequestHandler(StreamRequestHandler):
    '''Handler for TCPServer'''
    def __init__(self, request, client_address, server):
        self.queue = server.queue
        LOG.debug("Starting QueuedTCPRequestHandler")
        StreamRequestHandler.__init__(self, request, client_address, server)

    def handle(self):
        client_addr = self.client_address[0].encode().decode()
        for line in self.rfile:
            LOG.debug(f"[tcp] Received from {client_addr}: {line}")
            self.queue.put((client_addr, line))

    def finish(self):
        self.request.close()

class SyslogDaemon(object):
    def __init__(self):
        self.parse_queue = JoinableQueue()
        self.send_queue = JoinableQueue()

        self.config = {}

        config_file = os.environ.get('SNOOZE_SYSLOG_CONFIG') or '/etc/snooze-server/syslog.yaml'
        config_file = Path(config_file)
        try:
            with config_file.open('r') as myfile:
                self.config = yaml.safe_load(myfile.read())
        except Exception as err:
            LOG.error("Error loading config: %s", err)

        if not isinstance(self.config, dict):
            self.config = {}

        # Config and defaults
        snooze_uri = self.config.get('snooze_server')
        self.api = Snooze(snooze_uri)

        parse_workers_pool = self.config.get('parse_workers', 4)
        send_workers_pool = self.config.get('send_workers', 4)

        self.listening_address = self.config.get('listening_address', '0.0.0.0')
        self.listening_port = self.config.get('listening_port', 1514)

        self.tcp_server = ThreadedTCPServer(
            self.parse_queue,
            self.config,
            (self.listening_address, self.listening_port),
            QueuedTCPRequestHandler,
        )
        self.udp_server = UDPServer(
            (self.listening_address, self.listening_port),
            UDPHandler,
        )
        self.udp_server.queue = self.parse_queue

        tcp_thread = Thread(target=self.tcp_server.serve_forever)
        udp_thread = Thread(target=self.udp_server.serve_forever)

        try:
            tcp_thread.start()
            udp_thread.start()
            parse_threads = self.start_parse_workers(parse_workers_pool)
            send_threads = self.start_send_workers(send_workers_pool)

            all_threads = [tcp_thread, udp_thread] + parse_threads + send_threads

            for thread in all_threads:
                thread.join()

        finally:
            LOG.info("Stopping TCP socket...")
            self.tcp_server.shutdown()
            LOG.info("Stopping TCP server thread...")
            tcp_thread.join()
            LOG.info("Stopping UDP socket...")
            self.udp_server.shutdown()
            LOG.info("Stopping UDP server thread...")
            udp_thread.join()
            LOG.info("Stopping parse workers...")
            self.stop_threads(self.parse_queue, parse_threads)
            LOG.info("Stopping send workers...")
            self.stop_threads(self.send_queue, send_threads)

    def start_parse_workers(self, worker_pool):
        threads = []
        for index in range(worker_pool):
            mythread = Thread(target=self.parse_worker, args=(index,))
            mythread.start()
            threads.append(mythread)
        return threads

    def start_send_workers(self, worker_pool):
        threads = []
        for index in range(worker_pool):
            mythread = Thread(target=self.send_worker, args=(index,))
            mythread.start()
            threads.append(mythread)
        return threads

    def parse_worker(self, index):
        '''A worker for parsing syslog syntax'''
        while True:
            args = self.parse_queue.get()
            if not args:
                LOG.info(f"Stopping parse worker {index}")
                break
            record = parse_syslog(*args)
            self.send_queue.put(record)

    def send_worker(self, index):
        '''A worker for sending records to Snooze'''
        while True:
            LOG.debug("[send_record] Waiting for queue")
            records = self.send_queue.get()
            if not records:
                LOG.info(f"Stopping send worker {index}")
                break
            for record in records:
                LOG.debug(f"Sending record to snooze: {record}")
                self.api.process(record)

    def stop_threads(self, queue, threads):
        for _ in threads:
            queue.put(None)
        for thread in threads:
            thread.join()

def main():
    LOG = logging.getLogger("snooze.syslog")
    try:
        LOG.info("Starting snooze syslog daemon")
        SyslogDaemon()
    except (SystemExit, KeyboardInterrupt):
        LOG.info("Exiting snooze syslog daemon")
        sys.exit(0)
    except Exception as e:
        LOG.error(e, exc_info=1)
        sys.exit(1)

main()
