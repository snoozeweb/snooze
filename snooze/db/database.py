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

class Database:
    '''Abstract class for the database backend'''
    def __init__(self, conf):
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
    def init_db(self, conf):
        '''Initialize the database connection'''

    @abstractmethod
    def create_index(self, collection, fields):
        '''Create indexes for a given collection, and a given list of fields'''

    @abstractmethod
    def search(self, collection, condition, nb_per_page=0, page_number=1, orderby='', asc=True):
        '''List the objects of a collection based on a condition'''

    @abstractmethod
    def delete(self, collection, condition, force):
        '''Delete a collection's objects based on a condition'''

    @abstractmethod
    def write(self, collection, obj, primary=None, duplicate_policy='update', update_time=True, constant=None):
        '''Write an object in a collection'''

    @abstractmethod
    def convert(self, condition, search_fields=[]):
        '''Convert a condition (search) into a query usable in the database backend'''
