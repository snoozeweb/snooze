#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import click
import requests
import os

def get_token_from_file():
    '''Attempt to retrieve the token from the default file'''
    if os.path.exists('./.snooze-token'):
        with open('./.snooze-token', 'r') as f:
            return f.read()
    else:
        return None

def get_token():
    return get_token_from_file()

def write_token_to_file(token):
    with open('./.snooze-token', 'w') as f:
        f.write(token)

@click.command()
@click.option('-t', '--token', prompt=True, hide_input=True)
def login(token):
    '''CLI command to login'''
    headers = {'Authorization': 'JWT {}'.format(token)}
    response = requests.get('http://localhost:5200/api/record', headers=headers)
    if response.status_code == 200:
        write_token_to_file(token)
    else:
        print("Bad authentication token")
