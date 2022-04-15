'''Nagios/Icinga compatible check for snooze server'''

import sys
import traceback
from typing import List, Tuple

import yaml
from pathlib import Path

import click
import requests
from requests.exceptions import ConnectionError

from snooze.utils.functions import ca_bundle

def thread_details(status: dict) -> List[str]:
    '''Compute the details of the check'''
    details = []
    details.append('')
    details.append('Threads:')
    for name, thread in status['threads'].items():
        if thread['alive']:
            details.append(f"[OK] Thread '{name}' is running")
        else:
            details.append(f"[CRITICAL] Thread '{name}' is dead")
    return details

def mq_details(status: dict) -> List[str]:
    '''Compute MQManager details'''
    details = []
    details.append('')
    details.append('Message queues:')
    for name, thread in status['mq']['threads'].items():
        if thread['alive']:
            details.append(f"[OK] Queue '{name}' thread is running")
        else:
            details.append(f"[CRITICAL] Queue '{name}' thread is dead")
    if not status['mq']['threads']:
        details.append('No queue')
    return details

def get_nagios_message(status: str) -> Tuple[str, int]:
    '''Return the message and exit code to use depending on the nagaios status'''
    if status == 'ok':
        return 'Snooze server is healthy', 0
    elif status == 'warning':
        return 'Snooze server has a warning', 1
    elif status == 'critical':
        return 'Snooze server is critical', 2
    elif status == 'unknown':
        return 'Unknown error', 3
    return f"Unknown status: {status}", 3

@click.command()
@click.option('-s', '--server', type=str, help="The URL to the snooze server API to query")
@click.option('-c', '--cacert', type=str, help="A custom CA certificate to pass to requests")
def check_snooze_server(server, cacert):
    '''Nagios/Icinga compatible check to get the health of the snooze server'''
    if not server:
        client_config_path = Path('/etc/snooze/client.yaml')
        if client_config_path.exists():
            with client_config_path.open(encoding='utf-8') as myfile:
                config = yaml.safe_load(myfile)
                server = config.get('server')
        else:
            raise Exception("No server passed in argument or found in config (/etc/snooze/client.yaml)")

    url = f"{server}/api/health"
    exit_code = 0
    message = ''
    details = []
    cacert = cacert or ca_bundle()
    try:
        resp = requests.get(url, verify=cacert, timeout=3)
        if resp.status_code in [200, 503]:
            status = resp.json()
            message, exit_code = get_nagios_message(status['health'])
            details += thread_details(status)
            details += mq_details(status)
        else:
            message = f"Unexpected HTTP status: {resp.status_code}"
            exit_code = 3
    except ConnectionError as err:
        message = f"Connection error: {err}"
        exit_code = 1
    except Exception as err:
        message = 'Unknown error'
        details = [f"{err.__class__.__name__}: {err}"]
        details += traceback.format_exc().splitlines()
        exit_code = 1

    print(message)
    if details:
        print("\n".join(details))
    sys.exit(exit_code)
