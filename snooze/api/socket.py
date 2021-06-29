import os
import json

import atexit
from socketserver import UnixStreamServer
from http.server import SimpleHTTPRequestHandler

from logging import getLogger
log = getLogger('snooze')

POSSIBLE_PATHS = [
    '/var/run/snooze/snooze.socket',
    "/var/run/user/{}/snooze/snooze.socket".format(os.getuid()),
    './snooze.socket',
]

class UnixSocketHttpServer(UnixStreamServer):
    def get_request(self):
        request, client_address = super(UnixSocketHttpServer, self).get_request()
        return (request, ["snooze_server", 0])

def prepare_socket(socket_path=None):
    '''
    Verify the socket can be opened in that directory and
    select a good default socket if the argument is null
    '''
    possible_socket_paths = POSSIBLE_PATHS
    if socket_path:
        possible_socket_paths.insert(0, socket_path)
    my_socket = None
    for path in possible_socket_paths:
        try:
            abspath = os.path.abspath(path)
            dirname = os.path.dirname(abspath)
            if not os.path.exists(dirname):
                os.makedirs(dirname)
            my_socket = abspath
            break
        except Exception as e:
            log.debug("Tried socket %s, failed with:%s", abspath, e)
            continue
    log.info('Socket %s is available', my_socket)
    return my_socket

class SocketServer:
    '''Class to manage the socket connection'''
    def __init__(self, jwt_engine, socket_path=None):
        socket_path = prepare_socket(socket_path)
        self.jwt_engine = jwt_engine
        self.socket_path = socket_path
        self.cleanup_socket()
        atexit.register(self.__del__)
        self.server = UnixSocketHttpServer(self.socket_path, self.get_handler())

    def __del__(self):
        self.cleanup_socket()

    def cleanup_socket(self):
        if os.path.exists(self.socket_path):
            os.remove(self.socket_path)

    def serve(self):
        '''Function to service forever on the socket'''
        log.debug("Starting Unix socket at %s", self.socket_path)
        self.server.serve_forever()

    def get_handler(self):
        '''Return the handler class to use to serve'''
        jwt_engine = self.jwt_engine
        class Handler(SimpleHTTPRequestHandler):
            '''Handler for the socket connection'''
            def do_GET(self):
                if self.path == '/root_token':
                    root_token = jwt_engine.get_auth_token({'name': 'root', 'method': 'root', 'permissions': ['rw_all']})
                    self.protocol_version = 'HTTP/1.1'
                    self.send_response(200, 'OK')
                    self.send_header('Content-type', 'application/json')
                    self.end_headers()
                    json_string = json.dumps({'root_token': root_token}).encode('utf-8')
                    bytes_data = bytes(json_string)
                    self.wfile.write(bytes_data)
            def log_message(self, fmt, *args):
                log.debug(fmt, *args)
        return Handler
