import click
import sys

from snooze.cli.root_token import root_token
from snooze.cli.login import login
from snooze.cli.record import record
from snooze.cli.snooze import snooze as snooze_cmd

@click.group()
def snooze():
    pass

snooze.add_command(login)
snooze.add_command(root_token)

snooze.add_command(record)
snooze.add_command(snooze_cmd)
