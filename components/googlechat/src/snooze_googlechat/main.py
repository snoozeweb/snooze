import logging
import socket
import sys

from snooze_googlechat.pubsub import PubSub
from snooze_googlechat.http_server import HttpServer
from snooze_googlechat.config import load_config

logging.basicConfig(
    stream=sys.stdout, format="%(levelname)-8s %(message)s", level=logging.INFO
)

log = logging.getLogger("snooze.googlechat")
logging.getLogger("google").setLevel(logging.WARNING)
logging.getLogger("googleapiclient").setLevel(logging.WARNING)

socket.setdefaulttimeout(10)


def main():
    cfg = load_config()
    if cfg is None:
        log.fatal("failed to load config")

    if cfg.debug:
        logging.getLogger("snooze.googlechat").setLevel(logging.DEBUG)

    PubSub(cfg).start()
    HttpServer(cfg).serve()


if __name__ == "__main__":
    main()
