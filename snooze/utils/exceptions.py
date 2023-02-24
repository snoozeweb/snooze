#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Module for referencing custom exceptions used by snooze'''

import traceback
import sys
from typing import Optional
from logging import getLogger

from snooze.utils.typing import Record

log = getLogger('snooze')

class DatabaseError(RuntimeError):
    '''Wrapper for database errors (putting more info about each query)'''
    def __init__(self, operation: str, details: dict, err: Exception):
        self.operation = operation
        self.details = details
        super().__init__(self, f"Database error during {operation} ({details}): {err}")
