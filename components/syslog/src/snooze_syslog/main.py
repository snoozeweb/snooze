"""Main class for managing the syslog daemon"""

import logging
import os
import sys
from threading import Event

import yaml

# import prometheus_client
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from queue import Queue, Empty
from snooze_client import Snooze

from snooze_syslog.parser import parse_syslog
from snooze_syslog.udp import UDPListener
from snooze_syslog.tcp import TCPListener
from snooze_syslog.types import LogEntry

LOG = logging.getLogger("snooze.syslog")
logging.basicConfig(format="%(name)s: %(levelname)s - %(message)s", level=logging.INFO)


def load_config():
    """Load the configuration file"""
    config = {}
    config_file = Path(
        os.environ.get("SNOOZE_SYSLOG_CONFIG") or "/etc/snooze/syslog.yaml"
    )
    try:
        with config_file.open("r") as myfile:
            config = yaml.safe_load(myfile.read())
    except Exception as err:
        LOG.warning("Error loading config: %s", err)

    if not isinstance(config, dict):
        config = {}

    return config


class SyslogDaemon:
    """Daemon for listening to syslog and sending to snooze"""

    def __init__(self):
        config = load_config()

        # Config and defaults
        self.queue: Queue[LogEntry] = Queue()
        self.exit = Event()

        debug = config.get("debug", False)
        print("Debug: %s" % debug)
        if debug:
            LOG.setLevel(logging.DEBUG)

        snooze_uri = config.get("snooze_server")
        self.api = Snooze(snooze_uri)

        self.workers = config.get("workers", 4)

        host = config.get("listening_address", "0.0.0.0")
        port = config.get("listening_port", 1514)

        # prometheus_port = config.get('prometheus_port', 9301)
        # prometheus_client.start_http_server(prometheus_port)

        self.listeners = [
            TCPListener(host, port, self.queue, config),
            UDPListener(host, port, self.queue),
        ]

    def run(self):
        """Start the daemon"""
        try:
            for listener in self.listeners:
                listener.start()
            with ThreadPoolExecutor(max_workers=4) as pool:
                LOG.info("Starting workers")
                while not self.exit.is_set():
                    try:
                        entry = self.queue.get(timeout=0.1)
                        pool.submit(self.worker, *entry)
                    except Empty:
                        continue
        except Exception as err:
            LOG.error(err)
            self.stop()
        except (SystemExit, KeyboardInterrupt) as err:
            LOG.info("Stopping workers")
            raise err

    def stop(self):
        """Stopping the daemon"""
        self.exit.set()
        for listener in self.listeners:
            listener.stop()

    def worker(self, client_addr: str, log):
        # Parsing records
        records = parse_syslog(client_addr, log)
        # Sending to snooze
        for record in records:
            try:
                LOG.debug("Sending record to snooze: %s", record)
                self.api.alert_with_defaults(record)
            except Exception as err:
                LOG.error("Error sending record: %s", err)
                continue


def main():
    """Main function running the syslog daemon"""
    LOG.info("Starting snooze syslog daemon")
    daemon = SyslogDaemon()
    try:
        daemon.run()
    except (SystemExit, KeyboardInterrupt):
        daemon.stop()
        sys.exit(0)
    except Exception as err:
        LOG.error(err, exc_info=True)
        sys.exit(1)


if __name__ == "__main__":
    main()
