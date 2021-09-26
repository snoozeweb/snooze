import click
import sys

from snooze.cli.root_token import root_token
from snooze.cli.login import login

@click.group()
def snooze():
    pass

snooze.add_command(root_token)
