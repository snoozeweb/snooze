#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Typing utils for snooze'''

from typing import NewType, List

from typing_extensions import Literal, TypedDict

RecordUid = NewType('RecordUid', str)
Record = NewType('Record', dict)
Rule = NewType('Rule', dict)
AggregateRule = NewType('AggregateRule', dict)
SnoozeFilter = NewType('SnoozeFilter', dict)

Config = NewType('Config', dict)
Condition = NewType('Condition', list)

Pagination = NewType('Pagination', dict)

DuplicatePolicy = Literal['insert', 'reject', 'replace', 'update']

class AuthorizationPolicy(TypedDict):
    '''A list of authorized policy for read and write'''
    read: List[str]
    write: List[str]
