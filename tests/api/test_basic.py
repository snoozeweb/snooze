#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

import os
import json
from logging import getLogger
from pathlib import Path

import pytest
from freezegun import freeze_time
from pytest_data.functions import use_data

log = getLogger('tests')

class TestBasicApi:
    '''A series of tests on records, but that will check the basic
    behavior of every API endpoints'''

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

class TestBasicApiModify:

    data = {
        'record': [{'a': '1', 'b': '2'}, {'c': '1', 'd': '2'}],
    }

    def test_api_modify_record(self, client):
        result = client.simulate_get('/api/record/["=", "a", "1"]').json
        result['data'][0]['a'] = '2'
        client.simulate_post('/api/record', json=result['data'])
        result = client.simulate_get('/api/record/["=", "a", "2"]').json
        assert result and result['data'][0].items() >= {'a': '2', 'b': '2'}.items()

class TestBasicApiDelete:

    data = {
        'record': [{'a': '1', 'b': '2'}, {'c': '1', 'd': '2'}],
    }

    def test_api_delete_record_id(self, client):
        uid = (client.simulate_get('/api/record/["=", "a", "1"]').json)['data'][0]['uid']
        client.simulate_delete('/api/record/' + uid)
        result_search = client.simulate_get('/api/record').json
        assert [x for x in result_search['data'] if x.items() >= {'a': '1', 'b': '2'}.items()] == []
        assert [x for x in result_search['data'] if x.items() >= {'c': '1', 'd': '2'}.items()] != []

class TestBasicApiSort:

    data = {
        'record': [{'a': '2', 'b': '1'}, {'a': '0', 'b': '3'}, {'a': '1', 'b': '2'}],
    }

    def test_search_record_sort_1(self, client):
        result = client.simulate_get('/api/record/[]/0/1/a/true').json
        assert result and result['data'][0].items() >= {'a': '0', 'b': '3'}.items()
    def test_search_record_sort_2(self, client):
        result = client.simulate_get('/api/record', query_string='orderby=b&asc=false').json
        assert result and result['data'][0].items() >= {'a': '0', 'b': '3'}.items()

