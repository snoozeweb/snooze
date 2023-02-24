#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''General objects for managing database backends'''

import os
import time
from importlib import import_module
from urllib.parse import urlparse
from abc import ABC, abstractmethod
from logging import getLogger
from threading import Event
from typing import List, Optional, Union, Dict, Tuple, Any

from typing_extensions import TypedDict

from snooze.utils.config import DatabaseConfig
from snooze.utils.typing import Condition
from snooze.utils.exceptions import DatabaseError
from snooze.utils.threading import SurvivingThread

log = getLogger('snooze.db')

class Pagination(TypedDict, total=False):
    '''A type hint for pagination options'''
    orderby: str
    nb_per_page: int
    page_nb: int
    asc: bool

def wrap_exception(function):
    '''Wrap an method exception so we get more information about the
    query that made it fail'''
    def wrapper(database: 'Database', collection: str, *args, **kwargs):
        try:
            return function(database, collection, *args, **kwargs)
        except Exception as err:
            details = {'collection': collection, 'args': args, 'kwargs': kwargs}
            raise DatabaseError(function.__name__, details, err) from err
    return wrapper

def get_database(config: DatabaseConfig):
    module = import_module(f"snooze.db.{config.type}.database")
    return module.BackendDB(config)

class Database(ABC):
    '''Abstract class for the database backend'''

    @abstractmethod
    def create_index(self, collection: str, fields: List[str]):
        '''Create indexes for a given collection, and a given list of fields'''

    @abstractmethod
    def search(self, collection: str, condition:Optional[Condition]=None, **pagination: Pagination) -> dict:
        '''List the objects of a collection based on a condition'''

    @abstractmethod
    def delete(self, collection: str, condition: Condition, force: bool) -> dict:
        '''Delete a collection's objects based on a condition'''

    @abstractmethod
    def write(self, collection: str, obj: Union[dict, List[dict]], primary: Optional[str] = None, duplicate_policy: str = 'update', update_time: bool = True, constant: Optional[str] = None):
        '''Write an object in a collection'''

    @abstractmethod
    def get_one(self, collection: str, search: dict):
        '''Get one element based on a simple key=value filter'''

    @abstractmethod
    def replace_one(self, collection: str, uid: str, obj: dict, update_time: bool = True):
        '''Insert an object if absent'''

    @abstractmethod
    def update_one(self, collection: str, uid: str, obj: dict, update_time: bool = True):
        '''Update an object with a partial object, or insert if absent'''

    @abstractmethod
    def convert(self, condition: Condition, search_fields: List[str] = []):
        '''Convert a condition (search) into a query usable in the database backend'''

class AsyncIncrement:
    '''An object representing an increment in a collection.
    Will keep track of increments locally, and can flush them.'''
    collection: str
    field: str
    increments: Dict[dict, int]

    def __init__(self, database: Database, collection: str, field: str, upsert=False):
        self.database = database
        self.collection = collection
        self.field = field
        self.upsert = upsert

        self.increments = {}

    def hash(self, search: dict) -> dict:
        '''Hash a dict search into something hashable as long as every value is
        hashable'''
        return tuple(zip(search.keys(), search.values()))

    def unhash(self, mytuple) -> dict:
        '''Return a dict to be used for searching from a hashed dict (into tuple)'''
        return dict(mytuple)

    def flush(self):
        '''Flush the saved increments to the database'''
        updates = []
        for search_tuple, value in self.increments.items():
            if value > 0:
                search = self.unhash(search_tuple)
                updates.append((search, {self.field: value}))
                self.increments[search_tuple] -= value
        if updates:
            self.database.bulk_increment(self.collection, updates, upsert=self.upsert)

    def increment(self, search: dict, value: int = 1):
        '''Increment an object by a value'''
        search_tuple = self.hash(search)
        self.increments.setdefault(search_tuple, 0)
        self.increments[search_tuple] += value

class AsyncDatabase(SurvivingThread):
    '''A thread that will flush some async operations to the database.
    Practical for increments or bulk writes.'''
    increments: Dict[str, AsyncIncrement]
    def __init__(self, database: Database, interval: int = 1, exit_event: Optional[Event] = None):
        self.database = database
        self.interval = interval
        self.increments = {}

        SurvivingThread.__init__(self, exit_event)

    def new_increment(self, obj: AsyncIncrement):
        '''Add a new increment to the async worker.
        Currently only supporting one increment per collection.'''
        self.increments[obj.collection] = obj

    def _flush(self):
        '''Flush the data to the database'''
        for obj in self.increments.values():
            obj.flush()

    def start_thread(self):
        while not self.exit.wait(0.1):
            self._flush()
            time.sleep(self.interval)
        # Flushing one last time before exiting
        self._flush()
        log.info('Stopped async database thread')
