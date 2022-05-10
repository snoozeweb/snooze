#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import dateutil
from datetime import datetime, timezone
from logging import getLogger

import pytest
import yaml
from freezegun import freeze_time
from tinydb import TinyDB, Query

from snooze.db.database import Database
from snooze.utils.config import FileConfig

log = getLogger('tests')

@pytest.fixture(scope='function')
def config(tmp_path):
    path = tmp_path / 'db.json'
    cfg = FileConfig(path=path)
    path.touch()
    yield cfg
    path.unlink()

@pytest.fixture
def db(config):
    return Database(config)

def test_tinydb_base(config):
    db = TinyDB(config.path)
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
    db.write('record', [{'a': 1, 'b': 2, 'd': 1},{'a': 30, 'b': 40, 'c': 'tata', 'd': 1}])
    search1 = ['AND', ['=', 'a', 1], ['!=', 'b', 40]]
    result1 = db.search('record', search1)['count']
    search2 = ['OR', ['=', 'a', 1], ['=', 'a', 30]]
    result2 = db.search('record', search2)['count']
    search3 = ['MATCHES', 'c', 'ta*']
    result3 = db.search('record', search3)['count']
    search4 = ['NOT', ['=', 'a', 1]]
    result4 = db.search('record', search4)['count']
    search5 = ['EXISTS', 'c']
    result5 = db.search('record', search5)['count']
    search6 = ['>', 'a', 1]
    result6 = db.search('record', search6)['count']
    search7 = ['<', 'c', 'toto']
    result7 = db.search('record', search7)['count']
    search8 = ['=', 'c', 'toto']
    result8 = db.search('record', search8)['count']
    search9 = ['AND', ['=', 'b', 2], ['OR', ['=', 'd', 2], ['=', 'd', 1]]]
    result9 = db.search('record', search9)['count']
    assert result1 == 1
    assert result2 == 2
    assert result3 == 1
    assert result4 == 1
    assert result5 == 1
    assert result6 == 1
    assert result7 == 1
    assert result8 == 0
    assert result9 == 1

def test_tinydb_search_and_or(db):
    db.write('record', [{'a': 1, 'b': 2, 'c': 3}, {'c': 3}])
    assert db.search('record', ['AND', ['=', 'a', 1], ['=', 'b', 2], ['=', 'c', 3]])['count'] == 1
    assert db.search('record', ['OR', ['!=', 'a', 1], ['!=', 'b', 2], ['=', 'c', 3]])['count'] == 2

def test_tinydb_search_contains(db):
    db.write('record', [{'a': ['00', '11', '22', 9]}, {'a': ['00', '1', '2']}, {'a': ['00', '1', '4']}, {'b': '5'}])
    result1 = db.search('record', ['CONTAINS', 'a', '1'])['data']
    result2 = db.search('record', ['CONTAINS', 'a', ['2', '4']])['data']
    result3 = db.search('record', ['CONTAINS', 'b', ['5', '1']])['data']
    result4 = db.search('record', ['CONTAINS', 'a', 9])['data']
    assert len(result1) == 3 and len(result2) == 3 and len(result3) == 1 and len(result4) == 1

def test_tinydb_search_in(db):
    db.write('record', [{'a': ['00', '11', '22', 9]}, {'a': ['00', '1', '2']}, {'a': ['00', '1', '4']}, {'b': 'test'}])
    result1 = db.search('record', ['IN', '1', 'a'])['data']
    result2 = db.search('record', ['IN', ['2', '4'], 'a'])['data']
    result3 = db.search('record', ['IN', 'test', 'b'])['data']
    result4 = db.search('record', ['IN', 9, 'a'])['data']
    assert len(result1) == 2 and len(result2) == 2 and len(result3) == 1 and len(result4) == 1

def test_tinydb_search_in_query(db):
    db.write('record', [{'b': [{'x': '00'}, {'y':'1'}, {'z':'2'}]}, {'a': [{'x': '00'}, {'y':'1'}, {'z':'2'}]}, {'a': [{'x': '00'}, {'y':'1'}, {'z':'4'}]}])
    result1 = db.search('record', ['IN', ['=', 'y', '1'], 'b'])['data']
    result2 = db.search('record', ['IN', ['=', 'y', '1'], 'a'])['data']
    result3 = db.search('record', ['IN', ['OR', ['=', 'z', '2'], ['=', 'z', '4']], 'a'])['data']
    assert len(result1) == 1 and len(result2) == 2 and len(result3) == 2

def test_tinydb_search_text(db):
    db.write('record', [{'myfield': [{'found':'mystring'}, {'mysearch': '0'}]}, {'a': {'found': 'myvalue'}}])
    assert len(db.search('record', ['SEARCH', 'field'])['data']) == 1
    assert len(db.search('record', ['SEARCH', 'string'])['data']) == 1
    assert len(db.search('record', ['SEARCH', 'value'])['data']) == 1
    assert len(db.search('record', ['SEARCH', 'found'])['data']) == 2
    assert len(db.search('record', ['OR', ['SEARCH', 'string'], ['SEARCH', 'value']])['data']) == 2
    assert len(db.search('record', ['SEARCH', 'unknown'])['data']) == 0

def test_tinydb_search_nested(db):
   db.write('record', [{'a': [1, 2], 'b': {'c': 2, 'd': 3}}])
   assert len(db.search('record', ['=', 'b.c', 2])['data']) == 1
   assert len(db.search('record', ['=', 'a.1', 2])['data']) == 1

def test_tinydb_search_page(db):
    log.debug(db.search('record')['data'])
    db.write('record', [{'a': '1', 'b': '2'},{'a': '2', 'b': '2'},{'a': '3', 'b': '2'},{'a': '4', 'b': '2'},{'a': '5', 'b': '2'}])
    search = ['=', 'b', '2']
    result1 = db.search('record', search, nb_per_page=2)['data']
    result2 = db.search('record', search, nb_per_page=2, page_number=3)
    assert len(result1) == 2 and len(result2['data']) == 1 and result2['count'] == 5

def test_tinydb_search_id(db):
    db.write('record', {'a': '1', 'b': '2'})
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]['uid']
    result = db.search('record', ['=', 'uid', uid])['data']
    assert len(result) == 1

def test_tinydb_delete(db):
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    db.write('record', {'a': '100', 'b': '400'})
    search1 = ['OR', ['=', 'a', '1'], ['=', 'a', '30']]
    count = db.delete('record', search1)['count']
    result = db.search('record', search1)['data']
    assert count == 2 and len(result) == 0

def test_tinydb_delete_id(db):
    uid = db.write('record', {'a': '1', 'b': '2'})['data']['added'][0]['uid']
    count = db.delete('record', ['=', 'uid', uid])['count']
    assert count == 1

def test_tinydb_delete_all_fail(db):
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    count = db.delete('record', [])['count']
    assert count == 0

def test_tinydb_delete_all_force(db):
    db.write('record', {'a': '1', 'b': '2'})
    db.write('record', {'a': '30', 'b': '40'})
    count = db.delete('record', {}, True)['count']
    assert count == 2

def test_tinydb_update_uid_with_primary(db):
    uid = db.write('record', {'a': '1', 'b': '2'}, 'a')['data']['added'][0]['uid']
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a'] = '2'
    updated = db.write('record', result, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '2', 'b': '2'}.items()

def test_tinydb_replace_uid_with_primary(db):
    uid = db.write('record', {'a': '1', 'b': '2'}, 'a')['data']['added'][0]['uid']
    result = db.search('record', ['=', 'uid', uid])['data']
    del result[0]['b']
    replaced = db.write('record', result, 'a', 'replace')['data']['replaced']
    assert len(replaced) == 1 and 'b' not in replaced[0]

def test_tinydb_update_uid_duplicate_primary(db):
    db.write('record', {'a': {'b': '1', 'c': '1'}}, 'a.b')
    uid = db.write('record', {'a': {'b': '2', 'c': '2'}}, 'a.b')['data']['added'][0]['uid']
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['a']['b'] = '1'
    rejected = db.write('record', result, 'a.b')['data']['rejected']
    assert len(rejected) == 1

def test_tinydb_update_uid_constant(db):
    uid = db.write('record', {'a': '1', 'b': '2', 'c': 3})['data']['added'][0]['uid']
    result = db.search('record', ['=', 'uid', uid])['data']
    result[0]['c'] = '4'
    updated = db.write('record', result, constant=['a','b'])['data']['updated']
    result[0]['b'] = '1'
    rejected = db.write('record', result, constant=['a','b'])['data']['rejected']
    assert len(updated) == 1 and len(rejected) == 1

def test_tinydb_update_primary_constant(db):
    db.write('record', {'a': '1', 'b': '2', 'c': 3}, 'a')['data']['added'][0]['uid']
    updated = db.write('record',  {'a': '1', 'b': '2', 'c': 4}, 'a', constant='b')['data']['updated']
    rejected = db.write('record', {'a': '1', 'b': '1', 'c': 4}, 'a', constant='b')['data']['rejected']
    assert len(updated) == 1 and len(rejected) == 1

def test_tinydb_primary_duplicate_update(db):
    db.write('record', {'a': '1', 'b': '2'})
    updated = db.write('record', {'a': '1', 'b': '3'}, 'a')['data']['updated']
    result = db.search('record')['data']
    assert len(result) == 1 and len(updated) == 1 and result[0].items() >= {'a': '1', 'b': '3'}.items()

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

def test_tinydb_primary_duplicate_replace(db):
    db.write('record', {'a': '1', 'b': '2'})
    replaced = db.write('record', {'a': '1'}, 'a', 'replace')['data']['replaced']
    assert len(replaced) == 1 and 'b' not in replaced[0]

def test_tinydb_sort(db):
    db.write('record', [{'a': '1', 'b': '2'},{'a': '3', 'b': '2'},{'a': '2', 'b': '2'},{'a': '5', 'b': '2'},{'a': '4', 'b': '2'}])
    result = db.search('record', orderby='a', asc=False)['data']
    assert result[0].items() >= {'a': '5', 'b': '2'}.items()

def test_tinydb_sort_unknown(db):
    db.write('record', [{'a': '1', 'b': '2'},{'a': '3', 'b': '2'},{'a': '2', 'b': '2'},{'a': '5', 'b': '2'},{'a': '4', 'b': '2'}])
    result = db.search('record', orderby='c', asc=False)['data']
    assert result[0].items() >= {'a': '4', 'b': '2'}.items()

def test_tinydb_cleanup_timeout(db):
    db.write('record', [{'a': '1', 'ttl': 0}, {'b': '1', 'ttl': 0}, {'c': '1', 'ttl': 1}, {'d': '1'}])
    deleted_count = db.cleanup_timeout('record')
    assert deleted_count == 2

def test_tinydb_cleanup_orphans(db):
    uids = [o['uid'] for o in db.write('record', [{'a': '1'}, {'b': '1'}])['data']['added']]
    db.write('comment', [{'record_uid': uids[0]}, {'record_uid': uids[1]}, {'record_uid': 'random'}])
    deleted_count = db.cleanup_orphans('comment', 'record_uid', 'record', 'uid')
    assert deleted_count == 1

def test_tinydb_cleanup_audit_logs(db):
    audits = [
        {'id': 'a', 'collection': 'rule', 'object_id': 'uid1', 'timestamp': '2022-01-01T10:00:00+09:00', 'action': 'added', 'username': 'john.doe', 'method': 'ldap'},
        {'id': 'b', 'collection': 'rule', 'object_id': 'uid2', 'timestamp': '2022-01-02T11:00:00+09:00', 'action': 'updated', 'username': 'root', 'method': 'root'},
        {'id': 'c', 'collection': 'rule', 'object_id': 'uid1', 'timestamp': '2022-01-03T12:00:00+09:00', 'action': 'added', 'username': 'test', 'method': 'local'},
        {'id': 'd', 'collection': 'rule', 'object_id': 'uid3', 'timestamp': '2022-01-04T13:00:00+09:00', 'action': 'updated', 'username': 'john.doe', 'method': 'ldap'},
        {'id': 'e', 'collection': 'rule', 'object_id': 'uid3', 'timestamp': '2022-01-04T14:00:00+09:00', 'action': 'updated', 'username': 'john.doe', 'method': 'ldap'},
        {'id': 'f', 'collection': 'rule', 'object_id': 'uid3', 'timestamp': '2022-01-04T15:00:00+09:00', 'action': 'deleted', 'username': 'john.doe', 'method': 'ldap'},
    ]
    with freeze_time('2022-01-04T12:00:00+0900'):
        interval = 3*24*3600 # 3 days
        db.cleanup_audit_logs(interval)
    s = db.search('audit', orderby='timestamp')['data']
    assert len(s) == 0
    with freeze_time('2022-01-10T12:00:00+0900'):
        for audit in audits:
            audit['date_epoch'] = dateutil.parser.parse(audit['timestamp']).astimezone().timestamp()
        db.write('audit', audits, update_time=False)
    with freeze_time('2022-01-10T12:00:00+0900'):
        interval = 3*24*3600 # 3 days
        db.cleanup_audit_logs(interval)
    s = db.search('audit', orderby='timestamp')['data']
    assert len(s) == 3
    assert sorted(x['id'] for x in s) == ['a', 'b', 'c']

def test_tinydb_update_fields(db):
    db.write('record', [{'a': '1'}, {'b': '1', 'c': '1'}, {'b': '1'}])
    fields = {'c': '2', 'd': '1'}
    total = db.update_fields('record', fields, ['=', 'b', '1'])
    result = db.search('record')['data']
    total_real = 0
    for record in result:
        if record.get('c') == '2' and record.get('d') == '1':
            total_real += 1
    assert total == total_real == 2

def test_tinydb_inc(db):
    db.inc('stats', 'metric_a')
    assert db.search('stats', ['=', 'key', 'metric_a'])['data'][0]['value'] == 1
    db.inc('stats', 'metric_a')
    db.inc('stats', 'metric_b')
    assert db.search('stats', ['=', 'key', 'metric_a'])['data'][0]['value'] == 2
    assert db.search('stats', ['=', 'key', 'metric_b'])['data'][0]['value'] == 1

def test_tinydb_inc_labels(db):
    db.inc('stats', 'metric_a')
    assert db.search('stats')['data'][0]['value'] == 1
    db.inc('stats', 'metric_a', {'source': 'syslog'})
    assert db.search('stats', ['=', 'key', 'metric_a__source__syslog'])['data'][0]['value'] == 1
    db.inc('stats', 'metric_a', {'source': 'syslog', 'type': 'db'})
    assert db.search('stats', ['=', 'key', 'metric_a__source__syslog'])['data'][0]['value'] == 2
    assert db.search('stats', ['=', 'key', 'metric_a__type__db'])['data'][0]['value'] == 1

def test_tinydb_compute_stats(db):
    date_from = datetime(2016, 3, 10, 0, tzinfo=timezone.utc)
    a = datetime(2016, 3, 1, 0, tzinfo=timezone.utc).timestamp()
    b = datetime(2016, 3, 13, 0, tzinfo=timezone.utc).timestamp()
    c = datetime(2016, 3, 13, 5, tzinfo=timezone.utc).timestamp()
    d = datetime(2016, 3, 14, 0, tzinfo=timezone.utc).timestamp()
    date_until = datetime(2016, 3, 20, 0, tzinfo=timezone.utc)
    stats = [
        {'date': a, 'key': 'a_qty', 'value': 1 },
        {'date': b, 'key': 'a_qty', 'value': 1 },
        {'date': b, 'key': 'b_qty', 'value': 10},
        {'date': c, 'key': 'a_qty', 'value': 2 },
        {'date': d, 'key': 'a_qty', 'value': 4 },
        {'date': d, 'key': 'b_qty', 'value': 40},
        ]
    db.write('stats', stats)
    results = db.compute_stats('stats', date_from, date_until, 'day')
    assert list(filter(lambda x: x['key'] == 'a_qty', results['data'][0]['data']))[0]['value'] == 3
    assert list(filter(lambda x: x['key'] == 'b_qty', results['data'][0]['data']))[0]['value'] == 10
    assert list(filter(lambda x: x['key'] == 'a_qty', results['data'][1]['data']))[0]['value'] == 4
    assert list(filter(lambda x: x['key'] == 'b_qty', results['data'][1]['data']))[0]['value'] == 40
