#!/usr/bin/python3.6

from snooze.db.database import Database

import pytest
from pathlib import Path
from os import remove

from logging import getLogger
log = getLogger('snooze.test.tinydb')

from tinydb import TinyDB, Query

import yaml

with open('./examples/local_db.yaml', 'r') as f:
    default_config = yaml.load(f.read())

@pytest.fixture(scope='function')
def tinydb_file():
    filename = './db_test.json'
    Path(filename).touch()
    yield filename
    remove(filename)

@pytest.fixture
def db(tinydb_file):
    config = {'type': 'file', 'path': tinydb_file}
    return Database(config)

def test_tinydb_base(tinydb_file):
    db = TinyDB(tinydb_file)
    db.insert({'a': '1'})
    db.insert({'b': '2'})
    Records = Query()
    log.debug(db.all())
    log.debug(db.search(Records['a'] == '1'))
    log.debug(db.search(Records['b'] == '2'))
    assert True

def test_tinydb_all(db):
    db.write('record', {'a': '1', 'b': '2'})
    assert db.search('record')['data'][0].items() >= {'a': '1', 'b': '2'}.items()

def test_tinydb_search(db):
    db.write('record', [{'a': 1, 'b': 2},{'a': 30, 'b': 40, 'c': 'tata'}])
    search1 = ['AND', ['=', 'a', 1], ['!=', 'b', 40]]
    result1 = db.search('record', search1)['data']
    search2 = ['OR', ['=', 'a', 1], ['=', 'a', 30]]
    result2 = db.search('record', search2)['data']
    search3 = ['MATCHES', 'c', 'ta*']
    result3 = db.search('record', search3)['data']
    search4 = ['NOT', ['=', 'a', 1]]
    result4 = db.search('record', search4)['data']
    search5 = ['EXISTS', 'c']
    result5 = db.search('record', search5)['data']
    search6 = ['>', 'a', 1]
    result6 = db.search('record', search6)['data']
    search7 = ['<', 'c', 'toto']
    result7 = db.search('record', search7)['data']
    assert len(result1) == 1 and len(result2) == 2 and len(result3) == 1 and len(result4) == 1 and len(result5) == 1 and len(result6) == 1 and len(result7) == 1

def test_tinydb_search_contains(db):
    db.write('record', [{'a': ['00', ['11', '22']]}])
    result = db.search('record', ['CONTAINS', 'a', '1'])['data']
    assert len(result) == 1

def test_tinydb_search_nested(db):
   db.write('record', [{'a': 1, 'b': {'c': 2, 'd': 3}}])
   assert len(db.search('record', ['=', 'b.c', 2])['data']) == 1

def test_tinydb_search_page(db):
    log.debug(db.search('record')['data'])
    db.write('record', [{'a': '1', 'b': '2'},{'a': '2', 'b': '2'},{'a': '3', 'b': '2'},{'a': '4', 'b': '2'},{'a': '5', 'b': '2'}])
    search = ['=', 'b', '2']
    result1 = db.search('record', search, 2)['data']
    result2 = db.search('record', search, 2, 3)
    assert len(result1) == 2 and len(result2['data']) == 1 and result2['count'] == 5

def test_tinydb_search_id(db):
    db.write('record', {'a': '1', 'b': '2'})
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    assert len(result) == 1

def test_tinydb_delete(db):
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    db.write('record', {'a': '100', 'b': '400'})
    search1 = ['OR', ['=', 'a', '1'], ['=', 'a', '30']]
    count = db.delete('record', search1)['data']
    result = db.search('record', search1)['data']
    assert count == 2 and len(result) == 0

def test_tinydb_delete_id(db):
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]
    count = db.delete('record', ['=', 'uid', uid])['data']
    assert count == 1

def test_tinydb_delete_all(db):
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    count = db.delete('record', [])['data']
    assert count == 0

def test_tinydb_update_uid_with_primary(db):
    uid = db.write('record', {'a': '1', 'b': '2'}, 'a')['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a'] = '2'
    updated = db.write('record', result, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '2', 'b': '2'}.items() and updated[0].items() >= {'a': '1', 'b': '2'}.items()

def test_tinydb_update_uid_duplicate_primary(db):
    db.write('record', {'a': '1', 'b': '2'}, 'a')
    uid = db.write('record', {'a': '2', 'b': '2'}, 'a')['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a'] = '1'
    rejected = db.write('record', result, 'a')['data']['rejected']
    assert len(rejected) == 1

def test_tinydb_primary_duplicate_update(db):
    db.write('record', {'a': '1', 'b': '2'})
    updated = db.write('record', {'a': '1', 'b': '3'}, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '1', 'b': '3'}.items() and updated[0].items() >= {'a': '1', 'b': '2'}.items()

def test_tinydb_primary_duplicate_insert(db):
    db.write('record', {'a': '1', 'b': '2'})
    added = db.write('record', {'a': '1', 'b': '3'}, 'a', 'insert')['data']['added']
    result = db.search('record')['data']
    assert len(result) == 2 and len(added) == 1

def test_tinydb_primary_duplicate_reject(db):
    db.write('record', {'a': '1', 'b': '2'})
    rejected = db.write('record', {'a': '1', 'b': '3'}, 'a', 'reject')['data']['rejected']
    assert len(rejected) == 1

def test_tinydb_multiple_primary_update(db):
    db.write('record', {'a': '1', 'b': '1'})
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '1', 'b': '3'})
    db.write('record', {'a': '1', 'b': '2', 'c': '3'}, 'a,b')
    result = db.search('record',  ['=', 'b', '2'])['data']
    assert len(result) == 1 and result[0].items() >= {'a': '1', 'b': '2', 'c': '3'}.items()

def test_tinydb_sort(db):
    db.write('record', [{'a': '1', 'b': '2'},{'a': '3', 'b': '2'},{'a': '2', 'b': '2'},{'a': '5', 'b': '2'},{'a': '4', 'b': '2'}])
    result = db.search('record', orderby='a', asc=False)['data']
    assert result[0].items() >= {'a': '5', 'b': '2'}.items()
