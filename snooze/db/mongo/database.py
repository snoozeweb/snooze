#!/usr/bin/python3.6

from snooze.db.database import Database
from copy import deepcopy
from logging import getLogger
log = getLogger('snooze.db.mongo')

import pymongo
import uuid
import time
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

    def write(self, collection, obj, primary = None, duplicate_policy='update', update_time=True):
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
            primary_list = primary.split(',')
        for o in tobj:
            o.pop('_id', None)
            primary_result = None
            if update_time:
                o['time_epoch'] = time.time()
            if primary and all(p in o for p in primary_list):
                primary_query = list(map(lambda a: {a: o[a]}, primary_list))
                if len(primary_list) > 1:
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
                    else:
                        log.debug("In {}, updating {}".format(collection, o))
                        self.db[collection].update_one({'uid': o['uid']}, {'$set': o})
                        updated.append(result)
                else:
                    log.error("UID {} not found. Skipping...".format(o['uid']))
                    rejected.append(o)
            elif primary:
                if primary_result:
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
            log.error("Cannot find collection {}".format(collection))
            return {'data': [], 'count': 0}

    def delete(self, collection, condition=[]):
        mongo_search = self.convert(condition)
        log.debug("Condition {} converted to mongo delete search {}".format(condition, mongo_search))
        log.debug("List of collections: {}".format(self.db.collection_names()))
        if collection in self.db.collection_names():
            if len(condition) == 0:
                results = 0
                log.debug("Too dangerous to delete everything. Aborting")
            else:
                results = self.db[collection].delete_many(mongo_search).deleted_count
                log.debug("Found {} item(s) to delete search {}".format(results, mongo_search))
            return {'data': results}
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
        elif operation == '<':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_dict = {key: {'$lt': newval}}
        elif operation == 'MATCHES':
            key, value = args
            return_dict = {key: {'$regex': value, "$options": "-i"}}
        elif operation == 'EXISTS':
            return_dict = {args[0]: {'$exists': True}}
        elif operation == 'CONTAINS':
            key, value = args
            return_dict = {key: {'$in': [re.compile(value, re.IGNORECASE)]}}
        else:
            raise OperationNotSupported(operation)
        return return_dict
