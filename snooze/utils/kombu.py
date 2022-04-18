#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''
Custom transport and channel for Mongodb, because the current implementation in kombu
is forcing us to use a connection string, which limits us to 64 characters.
This implementatation passed the MongoClient directly.
'''

import pymongo
from pymongo import MongoClient, errors

from kombu.transport.mongodb import Channel
from kombu.transport import virtual

class MongodbChannel(Channel):
    '''Patched version of mongodb channel class'''

    database = MongoClient()['snooze']
    from_transport_options = virtual.Channel.from_transport_options + ('database',)

    def _create_client(self):
        self._create_broadcast(self.database)
        self._ensure_indexes(self.database)
        return self.database

class MongodbTransport(virtual.Transport):
    '''Patched version of mongodb transport class'''
    Channel = MongodbChannel

    can_parse_url = False
    polling_interval = 1
    connection_errors = (
        virtual.Transport.connection_errors + (errors.ConnectionFailure,)
    )
    channel_errors = (
        virtual.Transport.channel_errors + (
            errors.ConnectionFailure,
            errors.OperationFailure)
    )
    driver_type = 'mongodb'
    driver_name = 'pymongo'

    implements = virtual.Transport.implements.extend(
        exchange_type=frozenset(['direct', 'topic', 'fanout']),
    )

    def driver_version(self):
        return pymongo.version
