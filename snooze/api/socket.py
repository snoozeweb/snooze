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
from threading import Thread, Event

import falcon
from waitress.adjustments import Adjustments
from waitress.server import UnixWSGIServer

from snooze.api.falcon import LoggerMiddleware
from datetime import datetime, timedelta

log = getLogger('snooze.api.socket')

class RootTokenRoute:
    '''A route for generating a root token'''
    def __init__(self, token_engine):
        self.token_engine = token_engine

    def on_get(self, req, resp):
        log.debug("Received root token request from client")
        payload = {'user': {'name': 'root', 'method': 'root', 'permissions': ['rw_all']}, 'iat': datetime.utcnow(), 'nbf': datetime.utcnow(), 'exp': datetime.utcnow() + timedelta(seconds=3600)}
        root_token = self.token_engine.sign(payload).decode()
        resp.content_type = falcon.MEDIA_JSON
        resp.media = {'root_token': root_token}
        resp.status = falcon.HTTP_200

def admin_api(token_engine):
    '''Return a falcon WSGI app for returning the root token. Only used by the unix socket'''
    api = falcon.API(middleware=[LoggerMiddleware()])
    api.add_route('/api/root_token', RootTokenRoute(token_engine))
    return api

class WSGISocketServer(Thread, UnixWSGIServer):
    '''Listen on a Unix socket and serve the application'''
    def __init__(self, api, path, exit_button=None):
        self.path = Path(path).absolute()
        self.timeout = 10
        self.exit_button = exit_button or Event()

        unix_socket_adj = Adjustments(unix_socket=str(self.path))
        UnixWSGIServer.__init__(self, api, adj=unix_socket_adj)

        Thread.__init__(self)

    def run(self):
        '''Override Thread method. Start the service'''
        log.info("Listening on %s", self.path)
        UnixWSGIServer.run(self)

    def stop(self):
        '''Gracefully stop the service'''
        log.info("Closing wsgi unix socket at %s", self.path)
        self.close()
        log.debug("Deleting unix socket at %s", self.path)
        self.path.unlink()
