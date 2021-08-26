#!/usr/bin/python3.6

from snooze.db.database import Database
from snooze.utils.functions import dig
from copy import deepcopy
from bson.code import Code
from logging import getLogger
log = getLogger('snooze.db.mongo')

import pymongo
import uuid
import datetime
import re
import os

class OperationNotSupported(Exception): pass

database = os.environ.get('DATABASE_NAME', 'snooze')

def test_contains(array, value):
    return any(value in a for a in flatten(array))

class BackendDB(Database):
    def init_db(self, conf):
        if 'DATABASE_URL' in os.environ:
            self.db = pymongo.MongoClient(os.environ.get('DATABASE_URL'))[database]
        else:
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
        return self.run_pipeline(collection, pipeline)

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
        return self.run_pipeline(collection, pipeline)

    def run_pipeline(self, collection, pipeline):
        aggregate_results = self.db[collection].aggregate(pipeline)
        ids = list(map(lambda doc: doc['_id'], aggregate_results))
        deleted_results = self.db[collection].delete_many({'_id': {"$in": ids}})
        log.debug('Removed {} documents in {}'.format(deleted_results.deleted_count, collection))
        return deleted_results.deleted_count

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
            if update_time:
                o['date_epoch'] = datetime.datetime.now().timestamp()
            if primary and all(dig(o, *p.split('.')) for p in primary):
                primary_query = list(map(lambda a: {a: dig(o, *a.split('.'))}, primary))
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
                    elif duplicate_policy == 'replace':
                        log.debug("In {}, replacing {}".format(collection, o))
                        self.db[collection].replace_one({'uid': o['uid']}, o)
                        replaced.append(o)
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
                        elif duplicate_policy == 'replace':
                            log.debug("In {}, replacing {}".format(collection, o))
                            self.db[collection].replace_one(primary_query, o)
                            replaced.append(o)
                        else:
                            log.debug("In {}, updating {}".format(collection, o))
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
                added.append(o['uid'])
                add_obj = False
                log.debug("In {}, inserting {}".format(collection, o))
        if len(obj_copy) > 0:
            self.db[collection].insert_many(obj_copy)
        return {'data': {'added': added, 'updated': updated, 'replaced': replaced, 'rejected': rejected}}

    def search(self, collection, condition=[], nb_per_page=0, page_number=1, orderby='$natural', asc=True):
        if orderby == '':
            orderby = '$natural'
        mongo_search = self.convert(condition)
        log.debug("Condition {} converted to mongo search {}".format(condition, mongo_search))
        log.debug("List of collections: {}".format(self.db.collection_names()))
        if collection in self.db.collection_names():
            if nb_per_page > 0:
                results = self.db[collection].find(mongo_search).skip((page_number-1)*nb_per_page if page_number-1>0 else 0).limit(nb_per_page).sort(orderby, 1 if asc else -1)
            else:
                results = self.db[collection].find(mongo_search).sort(orderby, 1 if asc else -1)
            total = results.count()
            results = list(results)
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
            return_dict = {'$and': [arg1, arg2]}
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
            search_operator = '$in'
            if not isinstance(key, list):
                key = [key]
            else:
                try:
                    saved_key = key
                    key = self.convert(key)
                    search_operator = '$elemMatch'
                except:
                    key = saved_key
            return_dict = {value: {search_operator: key}}
        elif operation == 'SEARCH':
            arg = args[0]
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
