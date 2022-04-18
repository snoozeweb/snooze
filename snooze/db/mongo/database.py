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
from typing import List, Optional, Union

import pymongo
from bson.code import Code
from bson.json_util import dumps

from snooze.db.database import Database, Pagination, wrap_exception
from snooze.utils.functions import dig
from snooze.utils.typing import Condition, Config

log = getLogger('snooze.db.mongo')

class OperationNotSupported(Exception):
    '''Raised when the search operator is not supported'''

database = os.environ.get('DATABASE_NAME', 'snooze')
DEFAULT_PAGINATION = {
    'orderby': r'$natural',
    'page_number': 1,
    'nb_per_page': 0,
    'asc': True,
}

class BackendDB(Database):
    '''Database backend for MongoDB'''

    name = 'mongo'

    def init_db(self, conf: Config):
        if 'DATABASE_URL' in os.environ:
            self.db = pymongo.MongoClient(os.environ.get('DATABASE_URL'))[database]
        else:
            self.db = pymongo.MongoClient(**conf)[database]
        self.search_fields = {}
        self.conf = conf
        log.debug("Initialized Mongodb with config %s", conf)
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

    def cleanup_orphans(self, collection: str, key: str, col_ref: str, key_ref: str) -> int:
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
        return self.run_pipeline(collection, pipeline)

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

    def run_pipeline(self, collection: str, pipeline: List[dict]) -> int:
        '''Execute a filter pipeline on a collection, and delete the resulting objects.
        Return the number of deleted objects'''
        aggregate_results = self.db[collection].aggregate(pipeline)
        ids = [doc['_id'] for doc in aggregate_results]
        if ids:
            deleted_results = self.db[collection].delete_many({'_id': {"$in": ids}})
            log.debug('Removed %d documents in %s', deleted_results.deleted_count, collection)
            return deleted_results.deleted_count
        else:
            return 0

    @wrap_exception
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
                obj_copy.append(tobj)
                obj_copy[-1]['uid'] = str(uuid.uuid4())
                added.append(tobj)
                add_obj = False
                log.debug("In %s, inserting %s", collection, tobj.get('uid', ''))
            if old:
                tobj['_old'] = old
        if len(obj_copy) > 0:
            self.db[collection].insert_many(obj_copy)
        return {'data': {'added': added, 'updated': updated, 'replaced': replaced, 'rejected': rejected}}

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

    def update_fields(self, collection: str, fields: List[str], condition: Condition = []):
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        log.debug("Update collection '%s' with fields '%s' based on the following search", collection, fields)
        total = 0
        if collection in self.db.collection_names():
            pipeline = [
                {"$match": mongo_search},
                {"$addFields": fields},
                {"$merge": {'into': collection, 'on': '_id'}},
            ]
            try:
                self.db[collection].aggregate(pipeline)
                total = self.db[collection].find(mongo_search).count()
            except Exception as err:
                log.exception(err)
                total = 0
        log.debug("Updated %d fields", total)
        return total

    @wrap_exception
    def search(self, collection: str, condition:Optional[Condition]=None, **pagination) -> dict:
        if condition is None:
            condition = []
        for key, value in DEFAULT_PAGINATION.items():
            if pagination.get(key) is None:
                pagination[key] = value
        page_number = pagination['page_number']
        nb_per_page = pagination['nb_per_page']
        orderby = pagination['orderby']
        asc = pagination['asc']
        asc_int = (1 if asc else -1)
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        if collection in self.db.collection_names():
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
    def delete(self, collection: str, condition:Optional[Condition]=None, force:bool=False):
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
            return_dict = {key: {'$regex': str(value), "$options": "-i"}}
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
                return_dict = {'$or': [{field: {'$regex': str(arg), "$options": "-i"}} for field in search_fields]}
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
