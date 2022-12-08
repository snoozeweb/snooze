#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''MongoDB related tools'''

import datetime
import os
import re
import uuid
from copy import deepcopy
from pathlib import Path
from logging import getLogger
from typing import List, Optional, Union, Tuple

import pymongo
from pymongo import UpdateOne
from bson.code import Code
from bson.json_util import dumps
from opentelemetry.trace import get_tracer, get_current_span
from opentelemetry.instrumentation.pymongo import PymongoInstrumentor

from snooze.db.database import Database, Pagination, wrap_exception
from snooze.utils.functions import dig
from snooze.utils.typing import Condition
from snooze.utils.config import MongodbConfig
from snooze.tracing import MONGODB_TRACER

log = getLogger('snooze.db')
tracer = get_tracer('snooze')

# Instrument mongodb
PymongoInstrumentor().instrument(tracer_provider=MONGODB_TRACER)

class OperationNotSupported(Exception):
    '''Raised when the search operator is not supported'''

database = os.environ.get('DATABASE_NAME', 'snooze')
DEFAULT_PAGINATION = {
    'orderby': r'$natural',
    'page_number': 1,
    'nb_per_page': 0,
    'asc': True,
    'only_one': False,
}

def batch(cursor, batch_size: int = 100):
    '''Given a mongodb cursor, return a generator that yield a list of items
    of size that can be up to `batch_size`.
    Example:
    for chunk in batch(database.find({})):
        chunk: List[dict] = chunk
    '''
    index = 0
    cursor = cursor.batch_size(batch_size)
    while True:
        next_index = index + batch_size
        results = []
        for _ in range(batch_size):
            try:
                results.append(cursor.next())
            except StopIteration:
                yield results
                return
        yield results
        index = next_index

class BackendDB(Database):
    '''Database backend for MongoDB'''

    name = 'mongo'

    def __init__(self, config: MongodbConfig):
        self.db = pymongo.MongoClient(**config.dict(exclude={'type'}))[database]
        self.search_fields = {}
        log.debug("Initialized Mongodb")
        log.debug("db: %s", self.db)
        log.debug("List of collections: %s", self.db.collection_names())

    def create_index(self, collection: str, fields: List[str]):
        log.debug("Create index for %s with fields: %s", collection, fields)
        #self.db[collection].create_index(list(map(lambda x: (x, pymongo.ASCENDING), fields)), unique=True)
        self.search_fields[collection] = fields

    def cleanup_timeout(self, collection: str) -> int:
        now = datetime.datetime.now().timestamp()
        pipeline = [
            #{"$project":{ 'date_epoch':1, 'ttl':{ "$ifNull": ["$ttl", 0] }}},
            {"$match":{ 'ttl':{ "$gte":0 }}},
            {"$project":{ 'date_epoch':1, 'ttl':1, 'timeout':{ "$add": ["$date_epoch", "$ttl"] }}},
            {"$match":{ 'timeout':{ "$lte":now }}}
        ]
        return self.run_pipeline(collection, pipeline)

    def cleanup_comments(self):
        '''Delete comments which record doesn't exist anymore'''
        log.debug('cleanup_comments: Start')
        pipeline = [
            {'$group': {'_id': '$record_uid'}},
            {'$lookup': {'from': 'record', 'foreignField': 'uid', 'localField': '_id', 'as': 'matched'}},
            {'$match': {'matched': {'$eq': []}}},
        ]
        cursor = self.db['comment'].aggregate(pipeline)
        total_deleted = 0
        for chunk in batch(cursor, 100):
            record_uids = [comment['_id'] for comment in chunk if '_id' in comment]
            result = self.db['comment'].delete_many({'record_uid': {'$in': record_uids}})
            total_deleted += result.deleted_count
            log.debug("cleanup_comments: Removed %d comments", result.deleted_count)
        log.debug('cleanup_comments: Success')
        return total_deleted

    def cleanup_orphans(self, collection: str) -> int:
        '''Delete objects which one of their ancestors does not exist anymore'''
        log.debug("cleanup_orphans (%s): Start", collection)
        pipeline = [
            {'$addFields': {'parent': {'$last': "$parents"}}},
            {'$group': {'_id': None, 'parents': {'$addToSet': "$parent"}}},
        ]
        parents = list(self.db[collection].aggregate(pipeline))
        if len(parents) > 0:
            parents = [p for p in parents[0]['parents'] if p]
        if len(parents) == 0:
            log.debug('cleanup_orphans: Success (no parents)')
            return 0
        to_delete = []
        for parent in parents:
            if not self.db[collection].find_one({'uid': parent}):
                to_delete.append(parent)
        total = self.db[collection].delete_many({'parents': {'$in': to_delete}}).deleted_count
        log.debug("cleanup_orphans: Removed %d documents in %s", total, collection)
        return total

    def cleanup_audit_logs(self, interval: int):
        '''Cleanup audit logs of deleted objects'''
        log.info('Running audit log cleanup')
        now = datetime.datetime.now().astimezone().timestamp()
        date_threshold = now - interval
        log.debug("Threshold date: %s", datetime.datetime.fromtimestamp(date_threshold).astimezone())
        pipeline = [
            # Sort by most recent
            {'$sort': {'timestamp': -1}},
            # Get the last action for each object
            {'$group': {'_id': '$object_id', 'action': {'$first': '$action'}, 'date_epoch': {'$first': '$date_epoch'}}},
        ]
        ids = [
            o['_id']
            for o in self.db['audit'].aggregate(pipeline)
            if o.get('action') == 'deleted' \
            and o.get('date_epoch', 0) < date_threshold
        ]
        log.info("Found audit logs to remove for %d objects", len(ids))
        if ids:
            log.debug("Removing audit logs for %d objects", len(ids))
            self.db['audit'].delete_many({'object_id': {'$in': ids}})

    def renumber_field(self, collection, field):
        '''Renumber field by ascending order'''
        log.info("Reordering field '%s' in collection %s", field, collection)
        pipeline = [
            {'$sort': {field: 1}},
            {'$group': {'_id': 1, 'tmp_items': {'$push': '$$ROOT'}}},
            {'$unwind': {'path': '$tmp_items', 'includeArrayIndex': field}},
            {'$replaceWith': {'$mergeObjects': ['$tmp_items', {field: "$"+field}]}},
            {'$merge': { 'into': collection, 'on': '_id', 'whenMatched': 'replace'}},
        ]
        result = self.db[collection].aggregate(pipeline)
        log.info("Field '%s' renumbering on collection %s: Success", field, collection)

    def run_pipeline(self, collection: str, pipeline: List[dict]) -> int:
        '''Execute a filter pipeline on a collection, and delete the resulting objects.
        Return the number of deleted objects'''
        cursor = self.db[collection].aggregate(pipeline)
        total = 0
        for chunk in batch(cursor, 100):
            ids = [doc['_id'] for doc in chunk if '_id' in doc]
            if ids:
                deleted_results = self.db[collection].delete_many({'_id': {"$in": ids}})
                log.debug('Removed %d documents in %s', deleted_results.deleted_count, collection)
                total += deleted_results.deleted_count
        return total

    @wrap_exception
    @tracer.start_as_current_span('db.write')
    def write(self, collection:str, obj:Union[List[dict], dict], primary:Optional[str]=None, duplicate_policy:str='update', update_time:bool=True, constant:Optional[str]=None) -> dict:
        added = []
        rejected = []
        updated = []
        replaced = []
        obj_copy = []
        add_obj = False
        tobjs = deepcopy(obj)
        if not isinstance(tobjs, list):
            tobjs = [tobjs]
        if primary:
            if isinstance(primary , str):
                primary = primary.split(',')
        if constant:
            if isinstance(constant , str):
                constant = constant.split(',')
        for tobj in tobjs:
            tobj.pop('_id', None)
            tobj.pop('_old', None)
            primary_result = None
            old = {}
            if update_time:
                tobj['date_epoch'] = datetime.datetime.now().timestamp()
            if primary and all(dig(tobj, *p.split('.')) for p in primary):
                primary_query = [{a: dig(tobj, *a.split('.'))} for a in primary]
                if len(primary) > 1:
                    primary_query = {'$and': primary_query}
                else:
                    primary_query = primary_query[0]
                primary_result = self.db[collection].find_one(primary_query)
                if primary_result:
                    log.debug("Documents with same primary %s: %s", primary, primary_result.get('uid', ''))
            if 'uid' in tobj:
                result = self.db[collection].find_one({'uid': tobj['uid']})
                if result:
                    log.debug("UID %s found", tobj['uid'])
                    old = result
                    if primary_result and primary_result['uid'] != tobj['uid']:
                        error_message = f"Found another document with same primary {primary}: {primary_result}." \
                            "Since UID is different, cannot update"
                        log.error(error_message)
                        tobj['error'] = error_message
                        rejected.append(tobj)
                    elif constant and any(result.get(c, '') != tobj.get(c) for c in constant):
                        error_message = f"Found a document with existing uid {tobj['uid']} but different constant " \
                            f"values: {constant}. Since UID is different, cannot update"
                        log.error(error_message)
                        tobj['error'] = error_message
                        rejected.append(tobj)
                    elif duplicate_policy == 'replace':
                        log.debug("In %s, replacing %s", collection, tobj['uid'])
                        self.db[collection].replace_one({'uid': tobj['uid']}, tobj)
                        replaced.append(tobj)
                    else:
                        log.debug("In %s, updating %s", collection, tobj['uid'])
                        self.db[collection].update_one({'uid': tobj['uid']}, {'$set': tobj})
                        updated.append(tobj)
                else:
                    error_message = f"UID {tobj['uid']} not found. Skipping..."
                    log.error(error_message)
                    tobj['error'] = error_message
                    rejected.append(tobj)
            elif primary:
                if primary_result:
                    old = primary_result
                    if constant and any(primary_result.get(c, '') != tobj.get(c) for c in constant):
                        error_message = f"Found a document with existing primary {primary} but different " \
                            f"constant values: {constant}. Since UID is different, cannot update"
                        log.error(error_message)
                        tobj['error'] = error_message
                        rejected.append(tobj)
                    else:
                        log.debug('Evaluating duplicate policy: %s', duplicate_policy)
                        if duplicate_policy == 'insert':
                            add_obj = True
                        elif duplicate_policy == 'reject':
                            error_message = f"Another object exist with the same {primary}"
                            tobj['error'] = error_message
                            rejected.append(tobj)
                        elif duplicate_policy == 'replace':
                            log.debug("In %s, replacing %s", collection, primary_result.get('uid', ''))
                            if 'uid' in primary_result:
                                tobj['uid'] = primary_result['uid']
                            self.db[collection].replace_one(primary_query, tobj)
                            replaced.append(tobj)
                        else:
                            log.debug("In %s, updating %s", collection, primary_result.get('uid', ''))
                            self.db[collection].update_one(primary_query, {'$set': tobj})
                            updated.append(tobj)
                else:
                    log.debug("Could not find document with primary %s. Inserting instead", primary)
                    add_obj = True
            else:
                add_obj = True
            if add_obj:
                if 'uid' not in tobj:
                    tobj['uid'] = str(uuid.uuid4())
                obj_copy.append(tobj)
                added.append(tobj)
                add_obj = False
                log.debug("In %s, inserting %s", collection, tobj.get('uid', ''))
            if old:
                tobj['_old'] = old
        if len(obj_copy) > 0:
            self.db[collection].insert_many(obj_copy)
        return {'data': {'added': added, 'updated': updated, 'replaced': replaced, 'rejected': rejected}}

    @wrap_exception
    @tracer.start_as_current_span('db.update_one')
    def update_one(self, collection: str, uid: str, obj: dict, update_time: bool = True):
        span = get_current_span()
        span.set_attribute('collection', collection)
        span.set_attribute('uid', uid)
        new_obj = dict(obj)
        new_obj.pop('_id', None)
        if update_time:
            new_obj['date_epoch'] = datetime.datetime.now().timestamp()
        update = {
            '$set': new_obj,
            '$setOnInsert': {'uid': uid},
        }
        self.db[collection].update_one({'uid': uid}, update, upsert=True)

    @wrap_exception
    @tracer.start_as_current_span('db.get_one')
    def get_one(self, collection, search: dict):
        span = get_current_span()
        span.set_attribute('collection', collection)
        span.set_attribute('search', str(search))
        result = self.db[collection].find_one(search)
        return result

    @wrap_exception
    @tracer.start_as_current_span('db.replace_one')
    def replace_one(self, collection: str, search: dict, obj: dict, update_time: bool = True):
        span = get_current_span()
        span.set_attribute('collection', collection)
        span.set_attribute('search', str(search))
        new_obj = dict(obj)
        new_obj.pop('_id', None)
        for key, value in search.items():
            new_obj[key] = value
        if update_time:
            new_obj['date_epoch'] = datetime.datetime.now().timestamp()
        return self.db[collection].replace_one(search, new_obj, upsert=True).matched_count

    @wrap_exception
    def inc(self, collection: str, field: str, labels: dict = {}):
        now = datetime.datetime.utcnow()
        now = now.replace(minute=0, second=0, microsecond=0)
        keys = []
        added = []
        updated = []
        if labels:
            for key, value in labels.items():
                keys.append(f"{field}__{key}__{value}")
        else:
            keys.append(field)
        for key in keys:
            result = self.db[collection].find_one({"$and": [{"date": now}, {"key": key}]})
            if result:
                result['value'] = result.get('value', 0) + 1
                self.db[collection].update_one({"$and": [{"date": now}, {"key": key}]}, {'$set': result})
                updated.append(result)
            else:
                result = {'date': now, 'type': 'counter', 'key': key}
                result['value'] = 1
                self.db[collection].insert_one(result)
                added.append(result)
        return {'data': {'added': added, 'updated': updated}}

    @wrap_exception
    def inc_many(self, collection: str, field: str, condition:Optional[Condition] = None, value: int = 1):
        if condition is None:
            condition = []
        mongo_search = self.convert(condition)
        total = 0
        if collection in self.db.collection_names():
            total = self.db[collection].update_many(mongo_search, {'$inc': {field: value}}).matched_count
        return total

    @wrap_exception
    def bulk_increment(self, collection: str, updates: List[Tuple[dict, dict]], upsert: bool = False):
        '''Perform a bulk update of increments. Each update should be a tuple of search and update'''
        requests = []
        for search, update in updates:
            new_update = dict(update)
            new_update.pop('_id', None)
            if upsert:
                update_one = UpdateOne(search, {'$inc': new_update, '$setOnInsert': search}, upsert=True)
            else:
                update_one = UpdateOne(search, {'$inc': new_update})
            requests.append(update_one)
        self.db[collection].bulk_write(requests)

    def update_with_operation(self, collection, operation, condition=[]):
        mongo_search = self.convert(condition)
        total = 0
        if collection in self.db.collection_names():
            try:
                total = self.db[collection].update_many(mongo_search, operation).modified_count
            except Exception as err:
                log.exception(err)
        log.debug("Updated %d document(s)", total)
        return total

    def set_fields(self, collection, fields, condition=[]):
        log.debug("Update collection '%s' with fields '%s' based on the following search '%s'", collection, fields, condition)
        return self.update_with_operation(collection, {'$set': fields}, condition)

    def append_list(self, collection, fields, condition=[]):
        log.debug("Append to collection '%s' fields '%s' based on the following search: '%s'", collection, fields, condition)
        return self.update_with_operation(collection, {'$push': {field: {'$each': values} for field, values in fields.items()}}, condition)

    def prepend_list(self, collection, fields, condition=[]):
        log.debug("Prepend to collection '%s' fields '%s' based on the following search: '%s'", collection, fields, condition)
        return self.update_with_operation(collection, {'$push': {field: {'$each': values, '$position': 0} for field, values in fields.items()}}, condition)

    def remove_list(self, collection, fields, condition=[]):
        log.debug("Remove from collection '%s' fields '%s' based on the following search: '%s'", collection, fields, condition)
        return self.update_with_operation(collection, {'$pull': {field: {'$in': values} for field, values in fields.items()}}, condition)

    @wrap_exception
    @tracer.start_as_current_span('db.search')
    def search(self, collection: str, condition:Optional[Condition]=None, **pagination) -> dict:
        span = get_current_span()
        span.set_attribute('collection', collection)
        span.set_attribute('condition', str(condition))
        span.set_attribute('pagination', str(pagination))
        if condition is None:
            condition = []
        for key, value in DEFAULT_PAGINATION.items():
            if pagination.get(key) is None:
                pagination[key] = value
        page_number = pagination['page_number']
        nb_per_page = pagination['nb_per_page']
        orderby = pagination['orderby']
        asc = pagination['asc']
        only_one = pagination['only_one']
        asc_int = (1 if asc else -1)
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        if collection in self.db.collection_names():
            if only_one:
                result = self.db[collection].find_one(mongo_search, sort=[(orderby, asc_int)])
                results = []
                total = 0
                if result:
                    results = [result]
                    total = 1
            else:
                if nb_per_page > 0:
                    to_skip = (page_number - 1) * nb_per_page if page_number - 1 > 0 else 0
                    results = self.db[collection] \
                        .find(mongo_search) \
                        .skip(to_skip) \
                        .limit(nb_per_page) \
                        .sort(orderby, asc_int)
                else:
                    results = self.db[collection].find(mongo_search).sort(orderby, asc_int)
                total = results.count()
                results = list(results)
            log.debug("Found %d result(s) for search %s in collection %s. Pagination options: %s",
                total, mongo_search, collection, pagination)
            return {'data': results, 'count': total}
        else:
            log.warning("Cannot find collection %s", collection)
            return {'data': [], 'count': 0}

    @wrap_exception
    @tracer.start_as_current_span('db.search')
    def delete(self, collection: str, condition:Optional[Condition]=None, force:bool=False):
        span = get_current_span()
        span.set_attribute('collection', collection)
        span.set_attribute('condition', str(condition))
        span.set_attribute('force', force)
        if condition is None:
            condition = []
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        if collection in self.db.collection_names():
            if len(condition) == 0 and not force:
                results_count = 0
                log.debug("Too dangerous to delete everything. Aborting")
            else:
                results = self.db[collection].delete_many(mongo_search)
                results_count = results.deleted_count
                log.debug("Found %d item(s) to delete in collection %s for search %s",
                    results_count, collection, mongo_search)
            return {'data': [], 'count': results_count}
        else:
            log.error("Cannot find collection %s", collection)
            return {'data': 0}

    def compute_stats(self, collection, date_from, date_until, group_by='hour'):
        log.debug("Compute metrics on `%s` from %s until %s grouped by %s", collection, date_from, date_until, group_by)
        date_from = date_from.replace(minute=0, second=0, microsecond=0)
        if collection not in self.db.collection_names():
            log.debug("Compute stats: collection %s does not exist", collection)
            return {'data': [], 'count': 0}
        if group_by == 'hour':
            date_format = '%Y-%m-%dT%H:00%z'
        elif group_by == 'day':
            date_format = '%Y-%m-%dT00:00%z'
        elif group_by == 'month':
            date_format = '%Y-%m-01T00:00%z'
        elif group_by == 'year':
            date_format = '%Y-01-01T00:00%z'
        elif group_by == 'week':
            date_format = '%Y-%VT00:00%z'
        elif group_by == 'weekday':
            date_format = '%u'
        else:
            date_format = '%Y-%m-%dT%H:00%z'
        pipeline = [
            {"$match": {"$and": [{"date": {"$gte": date_from}}, {"date": {"$lte": date_until}}]}},
            {"$addFields": {"date_range": {"$dateToString": {"format": date_format, "timezone": date_from.strftime("%z"), "date": "$date"}}}},
            {"$group": {"_id": { "id": "$date_range", "key": "$key"}, "value": {"$sum": "$value"}}},
            {"$group": {"_id": "$_id.id", "data": {"$push": {"key": "$_id.key", "value": "$value"}}}},
        ]
        try:
            results_agg = sorted(list(self.db[collection].aggregate(pipeline)), key=lambda d: d['_id'])
            count = len(results_agg)
            log.debug("Compute stats: Got %d results", count)
            return {'data': results_agg, 'count': count}
        except Exception as err:
            log.exception(err)
            return {'data': [], 'count': 0}

    def drop(self, collection):
        if collection in self.db.collection_names():
            self.db[collection].drop()

    def convert(self, condition: Condition, search_fields: list = []):
        '''Convert `Condition` type from snooze.utils to Mongodb
        compatible type of search'''
        if not condition:
            return {}
        operation, *args = condition
        if operation == 'AND':
            arguments = list(map(lambda a: self.convert(a, search_fields), args))
            return_dict = {'$and': arguments}
        elif operation == 'OR':
            arguments = list(map(lambda a: self.convert(a, search_fields), args))
            return_dict = {'$or': arguments}
        elif operation == 'NOT':
            arg = self.convert(args[0], search_fields)
            return_dict = {'$nor': [arg]}
        elif operation == '=':
            key, value = args
            return_dict = {key: value}
        elif operation == '!=':
            key, value = args
            return_dict = {key: {'$ne': value}}
        elif operation == '>':
            key, value = args
            return_dict = {key: {'$gt': value}}
        elif operation == '>=':
            key, value = args
            return_dict = {key: {'$gte': value}}
        elif operation == '<':
            key, value = args
            return_dict = {key: {'$lt': value}}
        elif operation == '<=':
            key, value = args
            return_dict = {key: {'$lte': value}}
        elif operation == 'MATCHES':
            key, value = args
            return_dict = {key: {'$regex': str(value), "$options": "i"}}
        elif operation == 'EXISTS':
            return_dict = {args[0]: {'$exists': True}}
        elif operation == 'CONTAINS':
            key, value = args
            if not isinstance(value, list):
                value = [value]
            reg_array = list(map(lambda x: re.compile(x, re.IGNORECASE) if isinstance(x, str) else x, value))
            return_dict = {key: {'$in': reg_array}}
        elif operation == 'IN':
            key, value = args
            search_operator = '$in'
            if not isinstance(key, list):
                key = [key]
            else:
                try:
                    saved_key = key
                    key = self.convert(key, search_fields)
                    search_operator = '$elemMatch'
                except Exception:
                    key = saved_key
            return_dict = {value: {search_operator: key}}
        elif operation == 'SEARCH':
            arg = args[0]
            if search_fields:
                return_dict = {'$or': [{field: {'$regex': str(arg), "$options": "i"}} for field in search_fields]}
                log.debug("Special search : %s", return_dict)
            else:
                search_text = Code("function() {"
                                   "    var deepIterate = function  (obj, value) {"
                                   "        for (var field in obj) {"
                                   "            if (typeof obj[field] == 'string' && obj[field].includes(value)) {"
                                   "                return true;"
                                   "            }"
                                   "            var found = false;"
                                   "            if (typeof obj[field] === 'object') {"
                                   "               found = deepIterate(obj[field], value);"
                                   "               if (found) { return true; }"
                                   "            }"
                                   "        }"
                                   "        return false;"
                                   "    };"
                                   "    return deepIterate(this, '" + str(arg) + "');"
                                   "}")
                return_dict = {'$where': search_text}
        else:
            raise OperationNotSupported(operation)
        return return_dict

    def backup(self, backup_path: str, backup_exclude: Optional[List[str]] = None):
        '''Export the database into a directory'''
        if backup_exclude is None:
            backup_exclude = []
        collections = [c for c in self.db.collection_names() if c not in backup_exclude]
        log.debug('Starting backup of %s', collections)
        for collection in collections:
            try:
                data = self.db[collection].find()
                jsonpath = Path(backup_path) / (collection + '.json')
                with jsonpath.open("wb") as jsonfile:
                    jsonfile.write(dumps(data).encode())
                log.info('Backup of %s succeeded', collection)
            except Exception as err:
                log.exception(err)
                log.error('Backup of %s failed: %s', collection, err)
                continue
