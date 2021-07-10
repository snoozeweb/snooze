#!/usr/bin/python3.6

from snooze.db.database import Database
from copy import deepcopy
from dateutil import parser
from logging import getLogger
log = getLogger('snooze.db.mongo')

import pymongo
import uuid
import datetime
import re

class OperationNotSupported(Exception): pass

database = 'snooze'

def test_contains(array, value):
    return any(value in a for a in flatten(array))

class BackendDB(Database):
    def init_db(self, conf):
        self.db = pymongo.MongoClient(**conf)[database]
        log.debug("Initialized Mongodb with config {}".format(conf))
        log.debug("db: {}".format(self.db))

    def cleanup_timeout(self, collection):
        log.debug("Cleanup collection {}".format(collection))
        now = datetime.datetime.now().timestamp()
        pipeline = [
            #{"$project":{ 'date_epoch':1, 'ttl':{ "$ifNull": ["$ttl", 0] }}},
            {"$match":{ 'ttl':{ "$gte":0 }}},
            {"$project":{ 'date_epoch':1, 'ttl':1, 'timeout':{ "$add": ["$date_epoch", "$ttl"] }}},
            {"$match":{ 'timeout':{ "$lte":now }}}
        ]
        return self.run_pipeline_delete(collection, pipeline)

    def cleanup_orphans(self, collection, key, col_ref, key_ref):
        log.debug("Cleanup collection {} by finding {} in collection {} matching {}".format(collection, key, col_ref, key_ref))
        pipeline = [{
    	    "$lookup": {
                'from': col_ref,
                'localField': key,
                'foreignField': key_ref,
                'as': "matched_docs"
            }
        },{
            "$match": { "matched_docs": { "$eq": [] } }
        }]
        return self.run_pipeline_delete(collection, pipeline)

    def run_pipeline_delete(self, collection, pipeline):
        aggregate_results = self.db[collection].aggregate(pipeline)
        ids = list(map(lambda doc: doc['_id'], aggregate_results))
        deleted_results = self.db[collection].delete_many({'_id': {"$in": ids}})
        log.debug('Removed {} documents in {}'.format(deleted_results.deleted_count, collection))
        return deleted_results.deleted_count

    def date_in(self, collection, expression):
        try:
            date_str = expression[0]
            date_parsed = parser.parse(date_str)
            hours_str = date_parsed.strftime('%H:%m')
            weekday = int(date_parsed.strftime('%w'))
            field = expression[1]
            pipeline = [
                {"$match": {'$or': [{field+'.weekdays': {'$exists': False}}, {field+'.weekdays': {'$in': [weekday]}}]}},
                {"$match": {'$or': [{field+'.datetime': {'$exists': False}}, {field+'.datetime.until': {'$gte': date_str}}]}},
                {"$match": {'$or': [{field+'.datetime': {'$exists': False}}, {field+'.datetime.from': {'$lte': date_str}}]}},
                {"$match": {'$or': [{field+'.time': {'$exists': False}}, {field+'.time.until': {'$gte': hours_str}}]}},
                {"$match": {'$or': [{field+'.time': {'$exists': False}}, {field+'.time.from': {'$lte': hours_str}}]}}
            ]
            return pipeline
        except Exception as e:
            log.exception(e)
            return None
        

    def write(self, collection, obj, primary = None, duplicate_policy='update', update_time=True, constant=None):
        added = []
        rejected = []
        updated = []
        obj_copy = []
        tobj = obj
        add_obj = False
        tobj = deepcopy(obj)
        if type(tobj) != list:
            tobj = [tobj]
        if primary:
            if isinstance(primary , str):
                primary = primary.split(',')
        if constant:
            if isinstance(constant , str):
                constant = constant.split(',')
        for o in tobj:
            o.pop('_id', None)
            primary_result = None
            if update_time:
                o['date_epoch'] = datetime.datetime.now().timestamp()
            if primary and all(p in o for p in primary):
                primary_query = list(map(lambda a: {a: o[a]}, primary))
                if len(primary) > 1:
                    primary_query = {'$and': primary_query}
                else:
                    primary_query = primary_query[0]
                primary_result = self.db[collection].find_one(primary_query)
                log.debug("Documents with same primary {}: {}".format(primary, primary_result))
            if 'uid' in o:
                result = self.db[collection].find_one({'uid': o['uid']})
                if result:
                    log.debug("UID {} found".format(o['uid']))
                    if primary_result and primary_result['uid'] != o['uid']:
                        log.error("Found another document with same primary {}: {}. Since UID is different, cannot update".format(primary, primary_result))
                        rejected.append(o)
                    elif constant and any(result.get(c, '') != o.get(c) for c in constant):
                        log.error("Found a document with existing uid {} but different constant values: {}. Since UID is different, cannot update".format(o['uid'], constant))
                        rejected.append(o)
                    else:
                        log.debug("In {}, updating {}".format(collection, o))
                        self.db[collection].update_one({'uid': o['uid']}, {'$set': o})
                        updated.append(result)
                else:
                    log.error("UID {} not found. Skipping...".format(o['uid']))
                    rejected.append(o)
            elif primary:
                if primary_result:
                    if constant and any(primary_result.get(c, '') != o.get(c) for c in constant):
                        log.error("Found a document with existing primary {} but different constant values: {}. Since UID is different, cannot update".format(primary, constant))
                        rejected.append(o)
                    else:
                        log.debug('Evaluating duplicate policy: {}'.format(duplicate_policy))
                        if duplicate_policy == 'insert':
                            add_obj = True
                        elif duplicate_policy == 'reject':
                            rejected.append(o)
                        else:
                            log.debug("In {}, updating {}".format(collection, o))
                            self.db[collection].update_one(primary_query, {'$set': o})
                            updated.append(primary_result)
                else:
                    log.debug("Could not find document with primary {}. Inserting instead".format(primary))
                    add_obj = True
            else:
                add_obj = True
            if add_obj:
                obj_copy.append(o)
                obj_copy[-1]['uid'] = str(uuid.uuid4())
                added.append(o['uid'])
                add_obj = False
                log.debug("In {}, inserting {}".format(collection, o))
        if len(obj_copy) > 0:
            self.db[collection].insert_many(obj_copy)
        return {'data': {'added': added, 'updated': updated, 'rejected': rejected}}

    def search(self, collection, condition=[], nb_per_page=0, page_number=1, orderby='date_epoch', asc=True):
        if orderby == '':
            orderby = 'date_epoch'
        mongo_search = []
        if condition != []:
            if type(condition[0]) is dict:
                log.debug("Special condition detected")
                for cond in condition:
                    if cond.get('type', '') == 'match':
                        mongo_search.append({'$match': self.convert(cond.get('expression', []))})
                    elif cond.get('type', '') == 'date_in':
                        date_request = self.date_in(collection, cond.get('expression'))
                        if date_request:
                            mongo_search += date_request
            else:
                mongo_search.append({'$match': self.convert(condition)})
        log.debug("Condition {} converted to mongo search {}".format(condition, mongo_search))
        log.debug("List of collections: {}".format(self.db.collection_names()))
        if collection in self.db.collection_names():
            mongo_search.append({'$sort': {orderby: 1 if asc else -1}})
            facet = []
            if nb_per_page > 0:
                facet.append({'$skip': (page_number-1)*nb_per_page if page_number-1>0 else 0})
                facet.append({'$limit': nb_per_page})
            else:
                facet.append({'$skip': 0})
            mongo_search.append({'$facet': {'data': facet, 'count': [{'$count': 'count'}]}})
            agg_res = list(self.db[collection].aggregate(mongo_search))
            try:
                results = agg_res[0]['data']
                total = agg_res[0]['count'][0]['count']
            except Exception as e:
                results = []
                total = 0
            log.debug("Found {} for search {}. Page: {}-{}. Count: {}. Sort by {}. Order: {}".format(results, mongo_search, page_number, nb_per_page, total, orderby, 'Ascending' if asc else 'Descending'))
            return {'data': results, 'count': total}
        else:
            log.warning("Cannot find collection {}".format(collection))
            return {'data': [], 'count': 0}

    def delete(self, collection, condition=[], force=False):
        mongo_search = self.convert(condition)
        log.debug("Condition {} converted to mongo delete search {}".format(condition, mongo_search))
        log.debug("List of collections: {}".format(self.db.collection_names()))
        if collection in self.db.collection_names():
            if len(condition) == 0 and not force:
                results_count = 0
                log.debug("Too dangerous to delete everything. Aborting")
            else:
                results = self.db[collection].delete_many(mongo_search)
                results_count = results.deleted_count
                log.debug("Found {} item(s) to delete search {}".format(results_count, mongo_search))
            return {'data': [], 'count': results_count}
        else:
            log.error("Cannot find collection {}".format(collection))
            return {'data': 0}

    def convert(self, array):
        """
        Convert `Condition` type from snooze.utils
        to Mongodb compatible type of search
        """
        if not array:
            return {}
        operation, *args = array
        if operation == 'AND':
            arg1, arg2 = map(self.convert, args)
            return_dict = {**arg1, **arg2}
        elif operation == 'OR':
            arg1, arg2 = map(self.convert, args)
            return_dict = {'$or': [arg1, arg2]}
        elif operation == 'NOT':
            arg = self.convert(args[0])
            return_dict = {'$nor': [arg]}
        elif operation == '=':
            key, value = args
            return_dict = {key: value}
        elif operation == '!=':
            key, value = args
            return_dict = {key: {'$ne': value}}
        elif operation == '>':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_dict = {key: {'$gt': newval}}
        elif operation == '>=':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_dict = {key: {'$gte': newval}}
        elif operation == '<':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_dict = {key: {'$lt': newval}}
        elif operation == '<=':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_dict = {key: {'$lte': newval}}
        elif operation == 'MATCHES':
            key, value = args
            return_dict = {key: {'$regex': value, "$options": "-i"}}
        elif operation == 'EXISTS':
            return_dict = {args[0]: {'$exists': True}}
        elif operation == 'CONTAINS':
            key, value = args
            if not isinstance(value, list):
                value = [value]
            reg_array = list(map(lambda x: re.compile(x, re.IGNORECASE), value))
            return_dict = {key: {'$in': reg_array}}
        elif operation == 'IN':
            key, value = args
            if not isinstance(key, list):
                key = [key]
            return_dict = {value: {'$in': key}}
        else:
            raise OperationNotSupported(operation)
        return return_dict
