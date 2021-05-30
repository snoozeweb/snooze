#!/usr/bin/python3.6

import mongomock
from snooze.db.database import Database

import yaml

from logging import getLogger
log = getLogger('snooze.tests')

with open('./examples/mongo.yaml', 'r') as f:
    default_config = yaml.load(f.read())

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_all():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    assert db.search('record')['data'][0].items() >= {'a': '1', 'b': '2'}.items()

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_search():
    db = Database(default_config.get('database'))
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

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_search_contains():
    db = Database(default_config.get('database'))
    db.write('record', [{'a': ['00', '11', '22']}])
    result = db.search('record', ['CONTAINS', 'a', '1'])['data']
    assert len(result) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_search_nested():
   db = Database(default_config.get('database'))
   db.write('record', [{'a': 1, 'b': {'c': 2, 'd': 3}}])
   assert len(db.search('record', ['=', 'b.c', 2])['data']) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_search_page():
    db = Database(default_config.get('database'))
    db.write('record', [{'a': '1', 'b': '2'},{'a': '2', 'b': '2'},{'a': '3', 'b': '2'},{'a': '4', 'b': '2'},{'a': '5', 'b': '2'}])
    search = ['=', 'b', '2']
    result1 = db.search('record', search, 2)['data']
    result2 = db.search('record', search, 2, 3)
    assert len(result1) == 2 and len(result2['data']) == 1 and result2['count'] == 5

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_search_id():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    assert len(result) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_delete():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    db.write('record', {'a': '100', 'b': '400'})
    search1 = ['OR', ['=', 'a', '1'], ['=', 'a', '30']]
    count = db.delete('record', search1)['data']
    result = db.search('record', search1)['data']
    assert count == 2 and len(result) == 0

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_delete_id():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]
    count = db.delete('record', ['=', 'uid', uid])['data']
    assert count == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_delete_all():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    count = db.delete('record', {})['data']
    assert count == 0

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_update_uid_with_primary():
    db = Database(default_config.get('database'))
    uid = db.write('record', {'a': '1', 'b': '2'}, 'a')['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a'] = '2'
    updated = db.write('record', result, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '2', 'b': '2'}.items() and updated[0].items() >= {'a': '1', 'b': '2'}.items()

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_update_uid_duplicate_primary():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'}, 'a')
    uid = db.write('record', {'a': '2', 'b': '2'}, 'a')['data']['added'][0]
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a'] = '1'
    rejected = db.write('record', result, 'a')['data']['rejected']
    assert len(rejected) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_primary_duplicate_update():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    updated = db.write('record', {'a': '1', 'b': '3'}, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '1', 'b': '3'}.items() and updated[0].items() >= {'a': '1', 'b': '2'}.items()

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_primary_duplicate_insert():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    added = db.write('record', {'a': '1', 'b': '3'}, 'a', 'insert')['data']['added']
    result = db.search('record')['data']
    assert len(result) == 2 and len(added) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_primary_duplicate_reject():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '2'})
    rejected = db.write('record', {'a': '1', 'b': '3'}, 'a', 'reject')['data']['rejected']
    assert len(rejected) == 1

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_multiple_primary_update():
    db = Database(default_config.get('database'))
    db.write('record', {'a': '1', 'b': '1'})
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '1', 'b': '3'})
    db.write('record', {'a': '1', 'b': '2', 'c': '3'}, 'a,b')
    result = db.search('record',  ['=', 'b', '2'])['data']
    assert len(result) == 1 and result[0].items() >= {'a': '1', 'b': '2', 'c': '3'}.items()

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_sort():
    db = Database(default_config.get('database'))
    db.write('record', [{'a': '1', 'b': '2'},{'a': '3', 'b': '2'},{'a': '2', 'b': '2'},{'a': '5', 'b': '2'},{'a': '4', 'b': '2'}])
    result = db.search('record', orderby='a', asc=False)['data']
    assert result[0].items() >= {'a': '5', 'b': '2'}.items()

@mongomock.patch('mongodb://localhost:27017')
def test_mongo_cleanup():
    db = Database(default_config.get('database'))
    db.write('record', [{'a': '1', 'ttl': 0}, {'b': '1', 'ttl': 0}, {'c': '1', 'ttl': 1}, {'d': '1'}])
    deleted_count = db.cleanup('record')
    assert deleted_count == 2
