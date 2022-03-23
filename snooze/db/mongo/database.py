#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.db.database import Database
from snooze.utils.functions import dig
from copy import deepcopy
from bson.code import Code
from bson.json_util import dumps
from pathlib import Path
from logging import getLogger
log = getLogger('snooze.db.mongo')

import pymongo
import uuid
import datetime
import re
import os

class OperationNotSupported(Exception): pass

database = os.environ.get('DATABASE_NAME', 'snooze')

class BackendDB(Database):
    def init_db(self, conf):
        if 'DATABASE_URL' in os.environ:
            self.db = pymongo.MongoClient(os.environ.get('DATABASE_URL'))[database]
        else:
            self.db = pymongo.MongoClient(**conf)[database]
        self.search_fields = {}
        self.conf = conf
        log.debug("Initialized Mongodb with config {}".format(conf))
        log.debug("db: {}".format(self.db))
        log.debug("List of collections: {}".format(self.db.collection_names()))

    def create_index(self, collection, fields):
        log.debug("Create index for {} with fields: {}".format(collection, fields))
        #self.db[collection].create_index(list(map(lambda x: (x, pymongo.ASCENDING), fields)), unique=True)
        self.search_fields[collection] = fields

    def cleanup_timeout(self, collection):
        #log.debug("Cleanup collection {}".format(collection))
        now = datetime.datetime.now().timestamp()
        pipeline = [
            #{"$project":{ 'date_epoch':1, 'ttl':{ "$ifNull": ["$ttl", 0] }}},
            {"$match":{ 'ttl':{ "$gte":0 }}},
            {"$project":{ 'date_epoch':1, 'ttl':1, 'timeout':{ "$add": ["$date_epoch", "$ttl"] }}},
            {"$match":{ 'timeout':{ "$lte":now }}}
        ]
        return self.run_pipeline(collection, pipeline)

    def cleanup_orphans(self, collection, key, col_ref, key_ref):
        #log.debug("Cleanup collection {} by finding {} in collection {} matching {}".format(collection, key, col_ref, key_ref))
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

    def cleanup_audit_logs(self, interval):
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

    def run_pipeline(self, collection, pipeline):
        aggregate_results = self.db[collection].aggregate(pipeline)
        ids = list(map(lambda doc: doc['_id'], aggregate_results))
        if ids:
            deleted_results = self.db[collection].delete_many({'_id': {"$in": ids}})
            log.debug('Removed {} documents in {}'.format(deleted_results.deleted_count, collection))
            return deleted_results.deleted_count
        else:
            return 0

    def write(self, collection, obj, primary = None, duplicate_policy='update', update_time=True, constant=None):
        added = []
        rejected = []
        updated = []
        replaced = []
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
            old = {}
            if update_time:
                o['date_epoch'] = datetime.datetime.now().timestamp()
            if primary and all(dig(o, *p.split('.')) for p in primary):
                primary_query = list(map(lambda a: {a: dig(o, *a.split('.'))}, primary))
                if len(primary) > 1:
                    primary_query = {'$and': primary_query}
                else:
                    primary_query = primary_query[0]
                primary_result = self.db[collection].find_one(primary_query)
                if primary_result:
                    log.debug("Documents with same primary {}: {}".format(primary, primary_result.get('uid', '')))
            if 'uid' in o:
                result = self.db[collection].find_one({'uid': o['uid']})
                if result:
                    log.debug("UID {} found".format(o['uid']))
                    old = result
                    if primary_result and primary_result['uid'] != o['uid']:
                        error_message = f"Found another document with same primary {primary}: {primary_result}. Since UID is different, cannot update"
                        log.error(error_message)
                        o['error'] = error_message
                        rejected.append(o)
                    elif constant and any(result.get(c, '') != o.get(c) for c in constant):
                        error_message = f"Found a document with existing uid {o['uid']} but different constant values: {constant}. Since UID is different, cannot update"
                        log.error(error_message)
                        o['error'] = error_message
                        rejected.append(o)
                    elif duplicate_policy == 'replace':
                        log.debug("In {}, replacing {}".format(collection, o['uid']))
                        self.db[collection].replace_one({'uid': o['uid']}, o)
                        replaced.append(o)
                    else:
                        log.debug("In {}, updating {}".format(collection, o['uid']))
                        self.db[collection].update_one({'uid': o['uid']}, {'$set': o})
                        updated.append(o)
                else:
                    error_message = f"UID {o['uid']} not found. Skipping..."
                    log.error(error_message)
                    o['error'] = error_message
                    rejected.append(o)
            elif primary:
                if primary_result:
                    old = primary_result
                    if constant and any(primary_result.get(c, '') != o.get(c) for c in constant):
                        error_message = f"Found a document with existing primary {primary} but different constant values: {constant}. Since UID is different, cannot update"
                        log.error(error_message)
                        o['error'] = error_message
                        rejected.append(o)
                    else:
                        log.debug('Evaluating duplicate policy: {}'.format(duplicate_policy))
                        if duplicate_policy == 'insert':
                            add_obj = True
                        elif duplicate_policy == 'reject':
                            error_message = "Another object exist with the same {primary}"
                            o['error'] = error_message
                            rejected.append(o)
                        elif duplicate_policy == 'replace':
                            log.debug("In {}, replacing {}".format(collection, primary_result.get('uid', '')))
                            if 'uid' in primary_result:
                                o['uid'] = primary_result['uid']
                            self.db[collection].replace_one(primary_query, o)
                            replaced.append(o)
                        else:
                            log.debug("In {}, updating {}".format(collection, primary_result.get('uid', '')))
                            self.db[collection].update_one(primary_query, {'$set': o})
                            updated.append(o)
                else:
                    log.debug("Could not find document with primary {}. Inserting instead".format(primary))
                    add_obj = True
            else:
                add_obj = True
            if add_obj:
                obj_copy.append(o)
                obj_copy[-1]['uid'] = str(uuid.uuid4())
                added.append(o)
                add_obj = False
                log.debug("In {}, inserting {}".format(collection, o.get('uid', '')))
            o['_old'] = old
        if len(obj_copy) > 0:
            self.db[collection].insert_many(obj_copy)
        return {'data': {'added': added, 'updated': updated, 'replaced': replaced, 'rejected': rejected}}

    def inc(self, collection, field, labels={}):
        now = datetime.datetime.utcnow()
        now = now.replace(minute=0, second=0, microsecond=0)
        keys = []
        added = []
        updated = []
        if labels:
            for k,v in labels.items():
                keys.append(field+'__'+k+'__'+v)
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

    def update_fields(self, collection, fields, condition=[]):
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        log.debug("Update collection '{}' with fields '{}' based on the following search".format(collection, fields))
        #log.debug("Condition {} converted to mongo search {}".format(condition, mongo_search))
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
            except Exception as e:
                log.exception(e)
                total = 0
        log.debug("Updated {} fields".format(total))
        return total

    def search(self, collection, condition=[], nb_per_page=0, page_number=1, orderby='$natural', asc=True):
        if orderby == '':
            orderby = '$natural'
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        #log.debug("Condition {} converted to mongo search {}".format(condition, mongo_search))
        if collection in self.db.collection_names():
            if nb_per_page > 0:
                results = self.db[collection].find(mongo_search).skip((page_number-1)*nb_per_page if page_number-1>0 else 0).limit(nb_per_page).sort(orderby, 1 if asc else -1)
            else:
                results = self.db[collection].find(mongo_search).sort(orderby, 1 if asc else -1)
            total = results.count()
            results = list(results)
            log.debug("Found {} result(s) for search {} in collection {}. Page: {}-{}. Sort by {}. Order: {}".format(total, mongo_search, collection, page_number, nb_per_page, orderby, 'Ascending' if asc else 'Descending'))
            return {'data': results, 'count': total}
        else:
            log.warning("Cannot find collection {}".format(collection))
            return {'data': [], 'count': 0}

    def delete(self, collection, condition=[], force=False):
        mongo_search = self.convert(condition, self.search_fields.get(collection, []))
        #log.debug("Condition {} converted to mongo delete search {}".format(condition, mongo_search))
        if collection in self.db.collection_names():
            if len(condition) == 0 and not force:
                results_count = 0
                log.debug("Too dangerous to delete everything. Aborting")
            else:
                results = self.db[collection].delete_many(mongo_search)
                results_count = results.deleted_count
                log.debug("Found {} item(s) to delete in collection {} for search {}".format(results_count, collection, mongo_search))
            return {'data': [], 'count': results_count}
        else:
            log.error("Cannot find collection {}".format(collection))
            return {'data': 0}

    def compute_stats(self, collection, date_from, date_until, groupby='hour'):
        log.debug("Compute metrics on `{}` from {} until {} grouped by {}".format(collection, date_from, date_until, groupby))
        date_from = date_from.replace(minute=0, second=0, microsecond=0)
        if collection not in self.db.collection_names():
            log.debug("Compute stats: collection {} does not exist".format(collection))
            return {'data': [], 'count': 0}
        if groupby == 'hour':
            date_format = '%Y-%m-%dT%H:00%z'
        elif groupby == 'day':
            date_format = '%Y-%m-%dT00:00%z'
        elif groupby == 'month':
            date_format = '%Y-%m-01T00:00%z'
        elif groupby == 'year':
            date_format = '%Y-01-01T00:00%z'
        elif groupby == 'week':
            date_format = '%Y-%VT00:00%z'
        elif groupby == 'weekday':
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
            log.debug("Compute stats: Got {} results".format(count))
            return {'data': results_agg, 'count': count}
        except Exception as e:
            log.exception(e)
            return {'data': [], 'count': 0}

    def convert(self, array, search_fields = []):
        """
        Convert `Condition` type from snooze.utils
        to Mongodb compatible type of search
        """
        if not array:
            return {}
        operation, *args = array
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
                except:
                    key = saved_key
            return_dict = {value: {search_operator: key}}
        elif operation == 'SEARCH':
            arg = args[0]
            if search_fields:
                return_dict = {'$or': list(map(lambda field: {field: {'$regex': str(arg), "$options": "-i"}}, search_fields))}
                log.debug("Special search : {}".format(return_dict))
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

    def backup(self, backup_path, backup_exclude = []):
        collections = [c for c in self.db.collection_names() if c not in backup_exclude]
        log.debug('Starting backup of {}'.format(collections))
        try:
            for i, collection_name in enumerate(collections):
                col = getattr(self.db, collections[i])
                collection = col.find()
                jsonpath = Path(backup_path) / (collection_name + '.json')
                with jsonpath.open("wb") as jsonfile:
                    jsonfile.write(dumps(collection).encode())
        except Exception as e:
            log.error('Backup failed')
            log.exception(e)
        log.info('Backup of {} succeeded'.format(collections))

    def get_uri(self):
        if 'DATABASE_URL' in os.environ:
            return os.environ.get('DATABASE_URL')
        else:
            uri = 'mongodb://'
            if 'username' in self.conf or 'user' in self.conf:
                uri += self.conf.get('username', self.conf.get('user'))
            if 'password' in self.conf:
                uri += ':' + self.conf.get('password')
            if 'username' in self.conf or 'user' in self.conf:
                uri += '@'
            port = ''
            if 'port' in self.conf:
                port = ':' + str(self.conf.get('port'))
            if isinstance(self.conf.get('host'), list):
                uri += ','.join(self.conf.get('host')) + port
            else:
                uri += self.conf.get('host') + port
            uri += '/snooze'
            options = [op for op in ['authSource', 'replicaSet', 'tls', 'tlsCAFile'] if op in self.conf]
            if options:
                uri += '?' + '&'.join(list(map(lambda x: x + '=' + (str(self.conf.get(x, '')) if not isinstance(self.conf.get(x), bool) else str(self.conf.get(x)).lower()), options)))
            return uri

