#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

import json

import pytest
from pytest_data.functions import use_data
from freezegun import freeze_time

from logging import getLogger
log = getLogger('snooze.tests.api')

from pathlib import Path
import os

class TestApi:

    data = {
        'record': [{'a': '1', 'b': '2'}, {'c': '1', 'd': '2'}],
    }

    def test_search_all_records(self, client):
        result = client.simulate_get('/api/record').json
        assert result['data'][0]['a'] == '1'
    def test_search_record_1(self, client):
        result = client.simulate_get('/api/record/' + json.dumps(['=', 'a', '1'])).json
        assert result and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_2(self, client):
        result = client.simulate_get('/api/record/["=", "a", "1"]').json
        assert result and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_3(self, client):
        result = client.simulate_get('/api/record', query_string='s=["=", "a", "1"]').json
        assert result and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_id_1(self, client):
        uid = (client.simulate_get('/api/record').json)['data'][0]['uid']
        result = client.simulate_get('/api/record/' + uid).json
        assert result and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_id_2(self, client):
        uid = (client.simulate_get('/api/record').json)['data'][0]['uid']
        result = client.simulate_get('/api/record', query_string='s=' + uid).json
        assert result and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_page_1(self, client):
        result = client.simulate_get('/api/record/[]/1/1').json
        assert result and result['count'] == 2 and len(result['data']) == 1 and result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
    def test_search_record_page_2(self, client):
        result = client.simulate_get('/api/record/[]/1/2').json
        assert result and result['count'] == 2 and len(result['data']) == 1 and result['data'][0].items() >= {'c': '1', 'd': '2'}.items()
    def test_search_record_page_3(self, client):
        result = client.simulate_get('/api/record', query_string='s=[]&perpage=2&pagenb=1').json
        assert result
        assert result['count'] == 2
        assert len(result['data']) == 2
        assert result['data'][0].items() >= {'a': '1', 'b': '2'}.items()
        assert result['data'][1].items() >= {'c': '1', 'd': '2'}.items()

    def test_api_write_record(self, client):
        record = {'e': '1', 'f': '1'}
        client.simulate_post('/api/record', json=record)
        result = client.simulate_get('/api/record/["=", "e", "1"]').json
        assert result and result['data'][0].items() >= {'e': '1', 'f': '1'}.items()

    def test_api_delete_record(self, client):
        client.simulate_delete('/api/record/["=", "a", "1"]').json
        result_search = client.simulate_get('/api/record').json
        assert [x for x in result_search['data'] if x.items() >= {'a': '1', 'b': '2'}.items()] == []
        assert [x for x in result_search['data'] if x.items() >= {'c': '1', 'd': '2'}.items()] != []

    def test_api_delete_rule(self, client):
        client.simulate_post('/api/rule', json=[{'name': 'Rule0'}])
        result = client.simulate_get('/api/rule').json
        client.simulate_post('/api/rule', json={'name': 'Child', 'parent': result['data'][0]['uid']})
        client.simulate_delete('/api/rule/["=", "name", "Rule0"]').json
        result = client.simulate_get('/api/rule').json
        assert result['count'] == 0

    def test_api_post_rule_order(self, client):
        client.simulate_post('/api/rule', json=[{'name': 'Rule0'}, {'name': 'Rule1'}, {'name': 'Rule2'}])
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        for idx, rule in enumerate(result['data']):
            assert rule['tree_order'] == idx
            assert rule['name'] == 'Rule'+str(idx)
        client.simulate_post('/api/rule', json={'name': 'Rule3'})
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert result['data'][3]['tree_order'] == 3
        new_rules =  [{'name': 'Rule2_1', 'parent': result['data'][2]['uid']}, {'name': 'Rule2_0', 'parent': result['data'][2]['uid']}, {'name': 'Rule0_0', 'parent': result['data'][0]['uid']}]
        client.simulate_post('/api/rule', json=new_rules)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert list(map(lambda x: x['name'], result['data'])) == ['Rule0', 'Rule0_0', 'Rule1', 'Rule2', 'Rule2_0', 'Rule2_1', 'Rule3']
        new_rules =  [{'name': 'Rule3_0', 'parent': result['data'][6]['uid']}, {'name': 'Rule2_3', 'parent': result['data'][3]['uid']}, {'name': 'Rule2_2', 'parent': result['data'][3]['uid']}]
        client.simulate_post('/api/rule', json=new_rules)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert list(map(lambda x: x['name'], result['data'])) == ['Rule0', 'Rule0_0', 'Rule1', 'Rule2', 'Rule2_0', 'Rule2_1','Rule2_2', 'Rule2_3', 'Rule3', 'Rule3_0']

    def test_api_put_rule_order(self, client):
        client.simulate_post('/api/rule', json=[{'name': 'Rule0'}, {'name': 'Rule1'}, {'name': 'Rule2'}, {'name': 'Rule3'}])
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        new_rules =  [{'name': 'Rule2_1', 'parent': result['data'][2]['uid']}, {'name': 'Rule2_0', 'parent': result['data'][2]['uid']}, {'name': 'Rule0_0', 'parent': result['data'][0]['uid']}]
        client.simulate_post('/api/rule', json=new_rules)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        new_rules =  [{'name': 'Rule0_0_0', 'parent': result['data'][1]['uid']}, {'name': 'Rule2_1_1', 'parent': result['data'][5]['uid']}, {'name': 'Rule2_1_0', 'parent': result['data'][5]['uid']}]
        client.simulate_post('/api/rule', json=new_rules)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert list(map(lambda x: x['name'], result['data'])) == ['Rule0', 'Rule0_0', 'Rule0_0_0', 'Rule1', 'Rule2', 'Rule2_0', 'Rule2_1', 'Rule2_1_0', 'Rule2_1_1', 'Rule3']
        drag_rule0_0 = result['data'][1]
        drag_rule0_0['parent'] = result['data'][4]['uid']
        drag_rule0_0['insert_before'] = result['data'][6]['uid']
        client.simulate_put('/api/rule', json=drag_rule0_0)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert list(map(lambda x: x['name'], result['data'])) == ['Rule0', 'Rule1', 'Rule2', 'Rule2_0', 'Rule0_0', 'Rule0_0_0', 'Rule2_1', 'Rule2_1_0', 'Rule2_1_1', 'Rule3']
        drag_rule2_1 = result['data'][6]
        drag_rule2_1['parent'] = result['data'][1]['uid']
        drag_rule2_1['insert_after'] = result['data'][1]['uid']
        client.simulate_put('/api/rule', json=drag_rule2_1)
        result = client.simulate_get('/api/rule?orderby=tree_order').json
        assert list(map(lambda x: x['name'], result['data'])) == ['Rule0', 'Rule1', 'Rule2_1', 'Rule2_1_0', 'Rule2_1_1', 'Rule2', 'Rule2_0', 'Rule0_0', 'Rule0_0_0', 'Rule3']


class TestApi2:

    data = {
        'record': [{'a': '1', 'b': '2'}, {'c': '1', 'd': '2'}],
    }

    def test_api_modify_record(self, client):
        result = client.simulate_get('/api/record/["=", "a", "1"]').json
        result['data'][0]['a'] = '2'
        client.simulate_post('/api/record', json=result['data'])
        result = client.simulate_get('/api/record/["=", "a", "2"]').json
        assert result and result['data'][0].items() >= {'a': '2', 'b': '2'}.items()

class TestApi3:

    data = {
        'record': [{'a': '1', 'b': '2'}, {'c': '1', 'd': '2'}],
    }

    def test_api_delete_record_id(self, client):
        uid = (client.simulate_get('/api/record/["=", "a", "1"]').json)['data'][0]['uid']
        client.simulate_delete('/api/record/' + uid)
        result_search = client.simulate_get('/api/record').json
        assert [x for x in result_search['data'] if x.items() >= {'a': '1', 'b': '2'}.items()] == []
        assert [x for x in result_search['data'] if x.items() >= {'c': '1', 'd': '2'}.items()] != []

class TestApi4:

    data = {
        'record': [{'a': '2', 'b': '1'}, {'a': '0', 'b': '3'}, {'a': '1', 'b': '2'}],
    }

    def test_search_record_sort_1(self, client):
        result = client.simulate_get('/api/record/[]/0/1/a/true').json
        assert result and result['data'][0].items() >= {'a': '0', 'b': '3'}.items()
    def test_search_record_sort_2(self, client):
        result = client.simulate_get('/api/record', query_string='orderby=b&asc=false').json
        assert result and result['data'][0].items() >= {'a': '0', 'b': '3'}.items()

class TestApiAlert:

    data = {
        'record': [],
        'rule': [
            {'name': 'rule01', 'condition': ['MATCHES', 'host', '^myhost'], 'modifications': [['SET', 'role', 'myhost']]},
        ],
        'aggregaterule': [
            {'name': 'agg01', 'condition': ['=', 'source', 'syslog'], 'fields': ['host', 'message']},
        ],
        'snooze': [
            {'name': 'snooze01', 'condition': ['=', 'host', 'myhost02'], 'time_constraints': {}},
        ],
    }

    def test_alert_standard(self, client):
        alert = {'timestamp': '2021-08-30T09:00', 'source': 'syslog', 'host': 'myhost01', 'process': 'myapp', 'message': 'error found in process'}
        result = client.simulate_post('/api/alert', json=alert)
        assert result.status == '200 OK'
        uid = result.json['data']['added'][0]['uid']
        record = client.simulate_get('/api/record/' + uid).json['data'][0]
        assert record['host'] == 'myhost01'
        assert record['plugins'] == ['rule', 'aggregaterule', 'snooze', 'notification']
        assert record.get('snoozed') == None

    def test_alert_snooze(self, client):
        alert = {'timestamp': '2021-08-30T09:00', 'source': 'syslog', 'host': 'myhost02', 'process': 'myapp', 'message': 'error found in process'}
        result = client.simulate_post('/api/alert', json=alert)
        assert result.status == '200 OK'
        uid = result.json['data']['added'][0]['uid']
        record = client.simulate_get('/api/record/' + uid).json['data'][0]
        assert record['plugins'] == ['rule', 'aggregaterule', 'snooze']
        assert record['snoozed'] == 'snooze01'

    # Note: Time is frozen only for the /api/alert endpoint
    # because authenticated endpoints (/api/record) has a token from
    # `client` already, and it's time sensitive
#    def test_alert_aggregation(self, client):
#            alert1 = {'timestamp': '2021-08-30T09:00:00+0900', 'source': 'syslog', 'host': 'myhost03', 'process': 'myapp', 'message': 'error found in process'}
#            with freeze_time('2021-08-30T09:00:00+0900'):
#                result1 = client.simulate_post('/api/alert', json=alert1)
#            assert result1.status == '200 OK'
#            uid1 = result1.json['data']['added'][0]
#            record1 = client.simulate_get('/api/record/' + uid1).json['data'][0]
#            assert record1['aggregate'] == 'agg01'
#            assert record1['plugins'] == ['rule', 'aggregaterule', 'snooze', 'notification']
#
#            alert2 = {'timestamp': '2021-08-30T09:00:05+0900', 'source': 'syslog', 'host': 'myhost03', 'process': 'myapp', 'message': 'error found in process'}
#            with freeze_time('2021-08-30T09:00:05+0900'):
#                result2 = client.simulate_post('/api/alert', json=alert2)
#            assert result2.status == '200 OK'
#            print(result2.json)
#            uid2 = result2.json['data']['updated'][0]['uid']
#            record2 = client.simulate_get('/api/record/' + uid2).json['data'][0]
#
#            assert uid1 == uid2
#            print(record2)
#            assert record2['timestamp'] == '2021-08-30T09:00:05+0900'
#            assert record2['plugins'] == ['rule', 'aggregaterule']
#
#            alert3 = {'timestamp': '2021-08-30T09:15:00+0900', 'source': 'syslog', 'host': 'myhost03', 'process': 'myapp', 'message': 'error found in process'}
#            with freeze_time('2021-08-30T09:15:00+0900'):
#                result3 = client.simulate_post('/api/alert', json=alert3)
#            assert result3.status == '200 OK'
#            uid3 = result3.json['data']['updated'][0]['uid']
#            record3 = client.simulate_get('/api/record/' + uid3).json['data'][0]
#
#            assert uid1 == uid3
#            assert record3['timestamp'] == '2021-08-30T09:15:00+0900'
#            assert record3['plugins'] == ['rule', 'aggregaterule', 'snooze', 'notification']

# @mongomock.patch('mongodb://localhost:27017')
# def test_api_alert_simple():
#     core = Core(default_config)
#     api = Api(core)
#     alert = {"resource": "app:", "event": "UserNotice", "environment": "Production", "severity": "normal", "correlate": ["UserEmerg", "UserAlert", "UserCrit", "UserErr", "UserWarning", "UserNotice", "UserInfo", "UserDebug"],"service": ["Platform"], "group": "Syslog", "value": "notice", "text": "lulu\u0000", "tags": ["user.notice"], "attributes": {}, "origin": None, "type": None, "createTime": "2019-04-17T08:00:32.493Z", "timeout": None, "rawData": "<13>Apr 17 17:00:32 app: lulu\u0000", "customer": None}
#     client = testing.TestClient(api.handler)
#     headers = {'Authorization': 'JWT {}'.format(api.get_root_token())}
#     result = client.simulate_post('/alert', json=alert, headers=headers).status
#     assert result == '200 OK'
# 
# @mongomock.patch('mongodb://localhost:27017')
# def test_api_alert_rule():
#     core = Core(default_config)
#     api = Api(core)
#     rule = {'name': 'Rule1', 'condition': ['=', 'host', 'app'], 'modifications': [ ['SET', 'test_validated', 'True'] ]}
#     core.write('rule', rule)
#     alert = {"resource": "app:", "event": "UserNotice", "environment": "Production", "severity": "normal", "correlate": ["UserEmerg", "UserAlert", "UserCrit", "UserErr", "UserWarning", "UserNotice", "UserInfo", "UserDebug"],"service": ["Platform"], "group": "Syslog", "value": "notice", "text": "lulu\u0000", "tags": ["user.notice"], "attributes": {}, "origin": None, "type": None, "createTime": "2019-04-17T08:00:32.493Z", "timeout": None, "rawData": "<13>Apr 17 17:00:32 app: lulu\u0000", "customer": None}
#     client = testing.TestClient(api.handler)
#     headers = {'Authorization': 'JWT {}'.format(api.get_root_token())}
#     result = client.simulate_post('/alert', json=alert, headers=headers).status
#     search = ['=', 'test_validated', 'True']
#     assert result == '200 OK' and len(core.search('record', search)) == 1
# 
# def test_api_alert_snooze(core):
#     api = Api(core)
#     filt = {'name': 'Filter 1', 'condition': ['=', 'host', 'app']}
#     core.write('filters', filt)
#     alert = {"resource": "app:", "event": "UserNotice", "environment": "Production", "severity": "normal", "correlate": ["UserEmerg", "UserAlert", "UserCrit", "UserErr", "UserWarning", "UserNotice", "UserInfo", "UserDebug"],"service": ["Platform"], "group": "Syslog", "value": "notice", "text": "lulu\u0000", "tags": ["user.notice"], "attributes": {}, "origin": None, "type": None, "createTime": "2019-04-17T08:00:32.493Z", "timeout": None, "rawData": "<13>Apr 17 17:00:32 app: lulu\u0000", "customer": None}
#     client = testing.TestClient(api.handler)
#     headers = {'Authorization': 'JWT {}'.format(api.get_root_token())}
#     result = client.simulate_post('/alert', json=alert, headers=headers).status
#     search = ['=', 'snooze', True]
#     assert result == '200 OK' and len(core.search('record', search)) == 1
