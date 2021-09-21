'''CLI command for retrieving root token'''

import sys
from pathlib import Path
from urllib.parse import quote

import click
from requests_unixsocket import Session

@click.command()
@click.option('-s', '--socket', help='Force the socket which to connect', default='/var/run/snooze/server.socket')
def root_token(socket):
    '''main'''
    path = str(Path(socket).absolute())
    escaped_path = quote(path, safe='')
    uri = "http+unix://{}/api/root_token".format(escaped_path)
    response = Session().get(uri)
    token = response.json().get('root_token')
    print('Root token: {}'.format(token))
    if not token:
        sys.exit(1)
