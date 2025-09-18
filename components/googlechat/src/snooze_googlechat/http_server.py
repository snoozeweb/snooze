import falcon
from waitress.adjustments import Adjustments
from waitress.server import TcpWSGIServer
import logging
from pydantic import ValidationError

from snooze_googlechat.sender import Sender
from snooze_googlechat.config import Config
from snooze_googlechat.types import AlertWithURL

log = logging.getLogger("snooze.googlechat")


class HttpServer:
    def __init__(self, config: Config):
        level = logging.INFO
        self.config = config
        if config.debug:
            level = logging.DEBUG
        logging.basicConfig(
            format="%(asctime)s - %(name)s: %(levelname)s - %(message)s", level=level
        )
        sender = Sender(config)
        self.app = falcon.App()
        self.app.add_route("/alert", AlertRoute(sender))

    def serve(self):
        wsgi_options = Adjustments(
            host=self.config.listening_address, port=self.config.listening_port
        )
        httpd = TcpWSGIServer(self.app, adj=wsgi_options)
        log.info(f"Serving on port {self.config.listening_port}...")
        httpd.run()
        log.info("Shutting down...")


class AlertRoute:
    def __init__(self, sender: Sender):
        self.sender = sender
        self.url = sender.config.snooze_url

    def find_url(self, req) -> str:
        if self.url:
            return self.url
        elif hasattr(req, "forwarded_prefix") and req.forwarded_prefix:
            return req.forwarded_prefix
        else:
            return req.prefix

    def on_post(self, req, resp):
        medias = req.media
        if not isinstance(medias, list):
            medias = [medias]

        alerts = []
        for media in medias:
            try:
                media["url"] = self.find_url(req)
                alert = AlertWithURL.parse_obj(media)
                alerts.append(alert)
            except ValidationError as err:
                log.error(f"error validating alert: {err}")
                continue

        try:
            if len(alerts) == 0:
                resp.status = falcon.HTTP_400
                resp.text = "no alert provided"
                return
            response = None
            if len(alerts) == 1:
                response = self.sender.process_alert(alerts[0])
            else:
                response = self.sender.process_batch(alerts)
            resp.status = falcon.HTTP_200
            if response:
                resp.content_type = falcon.MEDIA_JSON
                resp.media = dict(response)
        except Exception as err:
            log.error(f"failed to process alerts: {err}")
            resp.status = falcon.HTTP_503
