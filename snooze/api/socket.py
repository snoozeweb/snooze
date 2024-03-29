#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''
A server running special functionalities on a unix socket
for bypassing authentication
'''

from logging import getLogger
from pathlib import Path
from threading import Event
from typing import Optional

import falcon
from waitress.adjustments import Adjustments
from waitress.server import UnixWSGIServer

from snooze.api import LoggerMiddleware
from snooze.utils.threading import SurvivingThread
from snooze.utils.functions import log_error_handler, log_warning_handler
from snooze.utils.typing import AuthPayload, HTTPUserErrors

log = getLogger('snooze.socket')

class RootTokenRoute:
    '''A route for generating a root token'''
    def __init__(self, token_engine):
        self.token_engine = token_engine

    def on_get(self, req, resp):
        log.debug("Received root token request from client")
        auth = AuthPayload(username='root', method='root', permissions=['rw_all'])
        root_token = self.token_engine.sign(auth)
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = {'root_token': root_token}

def admin_api(token_engine):
    '''Return a falcon WSGI app for returning the root token. Only used by the unix socket'''
    app = falcon.API(middleware=[LoggerMiddleware()])
    app.add_error_handler(HTTPUserErrors, log_warning_handler)
    app.add_error_handler(Exception, log_error_handler)
    app.add_route('/api/root_token', RootTokenRoute(token_engine))
    return app

class WSGISocketServer(SurvivingThread, UnixWSGIServer):
    '''Listen on a Unix socket and serve the application'''
    def __init__(self, api, path, exit_event: Optional[Event] = None):
        self.path = Path(path).absolute()
        self.timeout = 10
        self.exit_event = exit_event or Event()

        unix_socket_adj = Adjustments(unix_socket=str(self.path))
        UnixWSGIServer.__init__(self, api, adj=unix_socket_adj)

        SurvivingThread.__init__(self, exit_event)

    def start_thread(self):
        '''Override Thread method. Start the service'''
        log.info("Listening on %s", self.path)
        UnixWSGIServer.run(self)
        log.info('Stopped socket server')

    def stop_thread(self):
        '''Gracefully stop the service'''
        log.info("Closing wsgi unix socket at %s", self.path)
        self.close()
        log.debug("Deleting unix socket at %s", self.path)
        self.path.unlink(missing_ok=True)
