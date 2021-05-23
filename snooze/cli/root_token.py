from pathlib import Path
from requests_unixsocket import Session
from urllib.parse import quote

from snooze.api.socket import POSSIBLE_PATHS

import click
import sys

def find_socket(sock=None):
    '''Return the first socket that exists from the list of defaults'''
    possible_paths = POSSIBLE_PATHS
    if sock:
        possible_paths.insert(0, sock)
    for path in possible_paths:
        if Path(path).exists():
            return path
    print("Could not find any socket in {}".format(possible_paths))
    raise SystemExit


@click.command()
@click.option('-s', '--socket', help='Force the socket which to connect', default=None)
def root_token(socket=None):
    '''main'''
    socket = find_socket(socket)
    response = Session().get("http+unix://{}/root_token".format(quote(socket, safe='')))
    root_token = response.json().get('root_token')
    print('Root token: {}'.format(root_token))
    if not root_token:
        sys.exit(1)
