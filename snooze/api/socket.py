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

from datetime import datetime, timedelta
from logging import getLogger
from pathlib import Path
from threading import Event
from typing import Optional

import falcon
from waitress.adjustments import Adjustments
from waitress.server import UnixWSGIServer

from snooze.api.falcon import LoggerMiddleware
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.api.socket')

class RootTokenRoute:
    '''A route for generating a root token'''
    def __init__(self, token_engine):
        self.token_engine = token_engine

    def on_get(self, req, resp):
        log.debug("Received root token request from client")
        now = datetime.utcnow()
        payload = {
            'user': {'name': 'root', 'method': 'root', 'permissions': ['rw_all']},
            'iat': now,
            'nbf': now,
            'exp': now + timedelta(seconds=3600),
        }
        root_token = self.token_engine.sign(payload).decode()
        resp.content_type = falcon.MEDIA_JSON
        resp.status = falcon.HTTP_200
        resp.media = {'root_token': root_token}

def admin_api(token_engine):
    '''Return a falcon WSGI app for returning the root token. Only used by the unix socket'''
    api = falcon.API(middleware=[LoggerMiddleware()])
    api.add_route('/api/root_token', RootTokenRoute(token_engine))
    return api

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
        self.path.unlink()
