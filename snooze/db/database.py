#!/usr/bin/python3.6
import sys
from importlib import import_module

class Database():
    def __init__(self, conf):
        config = conf.copy()
        db_type = config.pop('type', 'file')
        cls = import_module("snooze.db.{}.database".format(db_type))
        self.__class__ = type('DB', (cls.BackendDB, Database), {})
        self.init_db(config)
    def init_db(self, conf): pass
    def search(self): pass
    def delete(self): pass
    def write(self): pass
