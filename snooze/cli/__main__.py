#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

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
