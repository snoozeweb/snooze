#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''General objects for managing database backends'''

import os
from importlib import import_module
from urllib.parse import urlparse
from abc import abstractmethod
from typing import List, Optional, Union

from typing_extensions import TypedDict

from snooze.utils.typing import Config, Condition
from snooze.utils.exceptions import DatabaseError

class Pagination(TypedDict, total=False):
    '''A type hint for pagination options'''
    orderby: str
    nb_per_page: int
    page_nb: int
    asc: bool

def wrap_exception(function):
    '''Wrap an method exception so we get more information about the
    query that made it fail'''
    def wrapper(collection: str, *args, **kwargs):
        try:
            return function(collection, *args, **kwargs)
        except Exception as err:
            details = {'collection': collection, 'args': args, 'kwargs': kwargs}
            raise DatabaseError(function.__name__, details, err) from err
    return wrapper

class Database:
    '''Abstract class for the database backend'''
    def __init__(self, conf: Config):
        config = conf.copy()
        db_type = config.pop('type', 'file')
        if 'DATABASE_URL' in os.environ:
            scheme = str(urlparse(os.environ.get('DATABASE_URL')).scheme)
            if scheme.startswith('mongodb'):
                db_type = 'mongo'
        cls = import_module(f"snooze.db.{db_type}.database")
        self.__class__ = type('DB', (cls.BackendDB, Database), {})
        self.init_db(config)

    @abstractmethod
    def init_db(self, conf: Config):
        '''Initialize the database connection'''

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
    def convert(self, condition: Condition, search_fields: List[str] = []):
        '''Convert a condition (search) into a query usable in the database backend'''
