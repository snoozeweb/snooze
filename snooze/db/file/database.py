#!/usr/bin/python3.6

from snooze.db.database import Database
from snooze.utils.functions import dig, flatten, to_tuple
from threading import Lock
from logging import getLogger
import uuid
import datetime
import re
log = getLogger('snooze.db.file')

from tinydb import TinyDB, Query as BaseQuery
from copy import deepcopy
from functools import reduce

class OperationNotSupported(Exception): pass

default_filename = 'db.json'
mutex = Lock()

class Query(BaseQuery):
    def test_root(self, func, *args):
        return self._generate_test(
            lambda value: func(value, *args),
            ('test', self._path, func, args),
            allow_empty_path=True
        )

def test_contains(array, value):
    if not isinstance(array, list):
        array = [array]
    for val in value:
        reg = re.compile(val, flags=re.IGNORECASE)
        for record in array:
            if reg.search(record):
                return True
    return False

def test_search(dic, value):
    return value in str(dic)

class BackendDB(Database):
    def init_db(self, conf):
        if conf.get('path'):
            filename = conf.get('path')
        else:
            filename = default_filename
        self.db = TinyDB(filename)
        log.debug("Initialized TinyDB at path {}".format(filename))
        log.debug("db: {}".format(self.db))

    def cleanup_timeout(self, collection):
        mutex.acquire()
        log.debug("Cleanup collection {}".format(collection))
        now = datetime.datetime.now().timestamp()
        aggregate_results = self.db.table(collection).search(Query().ttl >= 0)
        aggregate_results = list(map(lambda doc: {'_id': doc.doc_id, 'timeout': doc['ttl'] + doc['date_epoch']}, aggregate_results))
        aggregate_results = list(filter(lambda doc: doc['timeout'] <= now, aggregate_results))
        res = self.delete_aggregates(collection, aggregate_results)
        mutex.release()
        return res

    def cleanup_orphans(self, collection, key, col_ref, key_ref):
        mutex.acquire()
        log.debug("Cleanup collection {} by finding {} in collection {} matching {}".format(collection, key, col_ref, key_ref))
        results = list(map(lambda doc: doc[key_ref], self.db.table(col_ref).all()))
        aggregate_results = self.db.table(collection).search(~ (Query()[key].one_of(results)))
        aggregate_results = list(map(lambda doc: {'_id': doc.doc_id}, aggregate_results))
        res = self.delete_aggregates(collection, aggregate_results)
        mutex.release()
        return res

    def delete_aggregates(self, collection, aggregate_results):
        ids = list(map(lambda doc: doc['_id'], aggregate_results))
        deleted_results = self.db.table(collection).remove(doc_ids=ids)
        deleted_count = len(deleted_results)
        log.debug('Removed {} documents in {}'.format(deleted_count, collection))
        return deleted_count

    def write(self, collection, obj, primary = None, duplicate_policy='update', update_time=True, constant=None):
        mutex.acquire()
        added = []
        updated = []
        replaced = []
        rejected = []
        obj_copy = []
        tobj = obj
        add_obj = False
        table = self.db.table(collection)
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
            primary_docs = None
            if update_time:
                o['date_epoch'] = datetime.datetime.now().timestamp()
            if primary and all(dig(o, *p.split('.')) for p in primary):
                primary_query = map(lambda a: dig(Query(), *a.split('.')) == dig(o, *a.split('.')), primary)
                primary_query = reduce(lambda a, b: a & b, primary_query)
                primary_docs = table.search(primary_query)
                log.debug('Documents with same primary {}: {}'.format(primary, primary_docs))
            if 'uid' in o:
                query = Query()
                docs = table.search(query.uid == o['uid'])
                if docs:
                    doc = docs[0]
                    doc_id = doc.doc_id
                    log.debug('Found: {}'.format(doc))
                    if primary_docs and doc_id != primary_docs[0].doc_id:
                        log.error("Found another document with same primary {}: {}. Since UID is different, cannot update".format(primary, primary_docs))
                        rejected.append(o)
                    elif constant and any(doc.get(c, '') != o.get(c) for c in constant):
                        log.error("Found a document with existing uid {} but different constant values: {}. Since UID is different, cannot update".format(o['uid'], constant))
                        rejected.append(o)
                    elif duplicate_policy == 'replace':
                        log.debug('Replacing with: {}'.format(o))
                        self.db.table(collection).remove(doc_ids=[doc_id])
                        self.db.table(collection).insert(o)
                        replaced.append(o)
                    else:
                        log.debug('Updating with: {}'.format(o))
                        self.db.table(collection).update(o, doc_ids=[doc_id])
                        updated.append(doc)
                else:
                    log.error("UID {} not found. Skipping...".format(o['uid']))
                    rejected.append(o)
            elif primary:
                if primary_docs:
                    doc = primary_docs[0]
                    doc_id = doc.doc_id
                    if constant and any(doc.get(c, '') != o.get(c) for c in constant):
                        log.error("Found a document with existing primary {} but different constant values: {}. Since UID is different, cannot update".format(primary, constant))
                        rejected.append(o)
                    else:
                        log.debug('Evaluating duplicate policy: {}'.format(duplicate_policy))
                        if duplicate_policy == 'insert':
                            add_obj = True
                        elif duplicate_policy == 'reject':
                            rejected.append(o)
                        elif duplicate_policy == 'replace':
                            log.debug('Replace with: {}'.format(o))
                            self.db.table(collection).remove(doc_ids=[doc_id])
                            self.db.table(collection).insert(o)
                            replaced.append(o)
                        else:
                            log.debug('Update with: {}'.format(o))
                            self.db.table(collection).update(o, doc_ids=[doc_id])
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
            table.insert_multiple(obj_copy)
        mutex.release()
        return {'data': {'added': deepcopy(added), 'updated': deepcopy(updated), 'replaced': deepcopy(replaced),'rejected': deepcopy(rejected)}}

    def search(self, collection, condition=[], nb_per_page=0, page_number=1, orderby="", asc=True):
        mutex.acquire()
        tinydb_search = self.convert(condition)
        log.debug("Condition {} converted to tinydb search {}".format(condition, tinydb_search))
        log.debug("List of collections: {}".format(self.db.tables()))
        if collection in self.db.tables():
            table = self.db.table(collection)
            if tinydb_search:
                results = table.search(tinydb_search)
            else:
                results = table.all()
            total = len(results)
            if nb_per_page > 0:
                from_el = max((page_number-1)*nb_per_page, 0)
                to_el = page_number*nb_per_page
            else:
                from_el = None
                to_el = None
            if len(orderby) > 0 and all(dig(res, *orderby.split('.')) for res in list(results)):
                results = sorted(list(results),  key=lambda x: reduce(lambda c, k: c.get(k, {}), orderby.split('.'), x))
            if not asc:
                results = list(reversed(results))
            results = results[from_el:to_el]
            log.debug("Found {} for search {}. Page: {}-{}. Count: {}. Sort by {}. Order: {}".format(results, tinydb_search, page_number, nb_per_page, total, orderby, 'Ascending' if asc else 'Descending'))
            mutex.release()
            return {'data': deepcopy(results), 'count': total}
        else:
            log.warning("Cannot find collection {}".format(collection))
            mutex.release()
            return {'data': [], 'count': 0}

    def delete(self, collection, condition=[], force=False):
        mutex.acquire()
        tinydb_search = self.convert(condition)
        log.debug("Condition {} converted to tinydb delete search {}".format(condition, tinydb_search))
        log.debug("List of collections: {}".format(self.db.tables()))
        if collection in self.db.tables():
            table = self.db.table(collection)
            if len(condition) == 0 and not force:
                results_count = 0
                log.debug("Too dangerous to delete everything. Aborting")
            else:
                if len(condition) == 0:
                    results_count = len(table)
                    results = table.truncate()
                else:
                    results = table.remove(tinydb_search)
                    results_count = len(results)
                log.debug("Found {} item(s) to delete for search {}".format(results_count, tinydb_search))
            mutex.release()
            return {'data': [], 'count': results_count}
        else:
            mutex.release()
            log.error("Cannot find collection {}".format(collection))
            return {'data': 0}

    def convert(self, array):
        """
        Convert `Condition` type from snooze.utils
        to Mongodb compatible type of search
        """
        if not array:
            return None
        operation, *args = array
        if operation == 'AND':
            arg1, arg2 = map(self.convert, args)
            return_obj = arg1 & arg2
        elif operation == 'OR':
            arg1, arg2 = map(self.convert, args)
            return_obj = arg1 | arg2
        elif operation == 'NOT':
            arg = self.convert(args[0])
            return_obj = ~ arg
        elif operation == '=':
            key, value = args
            return_obj = dig(Query(), *key.split('.')) == value
        elif operation == '!=':
            key, value = args
            return_obj = dig(Query(), *key.split('.')) != value
        elif operation == '>':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_obj = dig(Query(), *key.split('.')) > newval
        elif operation == '>=':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_obj = dig(Query(), *key.split('.')) >= newval
        elif operation == '<':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_obj = dig(Query(), *key.split('.')) < newval
        elif operation == '<=':
            key, value = args
            try:
                newval = float(value)
            except ValueError:
                newval = value
            return_obj = dig(Query(), *key.split('.')) <= newval
        elif operation == 'MATCHES':
            key, value = args
            return_obj = dig(Query(), *key.split('.')).search(value, flags=re.IGNORECASE)
        elif operation == 'EXISTS':
            return_obj = dig(Query(), *args[0].split('.')).exists()
        elif operation == 'CONTAINS':
            key, value = args
            if not isinstance(value, list):
                value = [value]
            for val in value:
                return_obj = dig(Query(), *key.split('.')).test(test_contains, to_tuple(value))
        elif operation == 'IN':
            key, value = args
            if not isinstance(key, list):
                key = [key]
            else:
                try:
                    saved_key = key
                    key = self.convert(key)
                except:
                    key = saved_key
            return_obj = dig(Query(), *value.split('.')).any(key)
        elif operation == 'SEARCH':
            arg = args[0]
            return_obj = Query().test_root(test_search, to_tuple(arg))
        else:
            raise OperationNotSupported(operation)
        return return_obj
