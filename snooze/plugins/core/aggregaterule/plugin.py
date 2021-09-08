#!/usr/bin/python3.6

from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition, Modification
from snooze.utils.functions import dig

import datetime
import logging
import hashlib
from logging import getLogger
from copy import deepcopy
from snooze.plugins.core import Abort
LOG = getLogger('snooze.aggregaterule')


class Aggregaterule(Plugin):
    def process(self, record):
        """
        Process the record against a list of aggregate rules

        Args:
            record (dict)
        """
        LOG.debug("Processing record: {}".format(str(record)))
        for aggrule in self.aggregate_rules:
            if aggrule.enabled and aggrule.process(record):
                record['hash'] = hashlib.md5((str(aggrule.name) + '.'.join([(dig(record, *field.split('.')) or '') for field in aggrule.fields])).encode()).hexdigest()
                record = self.match_aggregate(record, aggrule.throttle, aggrule.watch)
                break
        else:
            LOG.debug("Record {} could not match any aggregate rule, assigning a default aggregate".format(str(record)))
            if 'raw' in record:
                if type(record['raw']) is dict:
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'].items())).encode('utf-8')).hexdigest()
                elif type(record['raw']) is list:
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'])).encode('utf-8')).hexdigest()
                else:
                    record['hash'] = hashlib.md5(record['raw'].encode('utf-8')).hexdigest()
            else:
                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
            record['aggregate'] = 'default'
            record = self.match_aggregate(record)

        return record

    def match_aggregate(self, record, throttle=10, watch=[]):
        LOG.debug("Checking if an aggregate with hash {} can be found".format(record['hash']))
        aggregate_result = self.db.search('record', ['=', 'hash', record['hash']])
        if aggregate_result['count'] > 0:
            aggregate = aggregate_result['data'][0]
            LOG.debug("Found {}, updating it with the record infos".format(str(aggregate)))
            now = datetime.datetime.now()
            record = dict(list(aggregate.items()) + list(record.items()))
            record['uid'] = aggregate.get('uid') 
            record['state'] = aggregate.get('state', '')
            record['duplicates'] = aggregate.get('duplicates', 0) + 1
            record['date_epoch'] = aggregate.get('date_epoch', now.timestamp())
            if aggregate.get('ttl', -1) < 0:
                record['ttl'] = aggregate.get('ttl', -1)
            watched_fields = []
            for watched_field in watch:
                aggregate_field = dig(aggregate, *watched_field.split('.'))
                record_field = dig(record, *watched_field.split('.'))
                LOG.debug("Watched field {}: compare {} and {}".format(watched_field, record_field, aggregate_field))
                if record_field != aggregate_field:
                    watched_fields.append({'name': watched_field, 'old': aggregate_field, 'new': record_field})
            if watched_fields:
                LOG.debug("Found updated fields from watchlist: {}".format(watched_fields))
                comment = {}
                comment['record_uid'] = aggregate['uid']
                comment['date'] = now.astimezone().isoformat()
                comment['auto'] = True
                append_txt = []
                for watch_field in watched_fields:
                    append_txt.append("{} ({} => {})".format(watch_field['name'], watch_field['old'], watch_field['new']))
                if record.get('state') == 'close':
                    comment['message'] = 'Auto re-opened from watchlist: {}'.format(', '.join(append_txt))
                    comment['type'] = 'open'
                    record['state'] = 'open'
                elif record.get('state') == 'ack':
                    comment['message'] = 'Auto re-escalated from watchlist: {}'.format(', '.join(append_txt))
                    comment['type'] = 'esc'
                    record['state'] = 'esc'
                else:
                    comment['message'] = 'New escalation from watchlist: {}'.format(', '.join(append_txt))
                    comment['type'] = 'comment'
                self.db.write('comment', comment)
                record['comment_count'] = aggregate.get('comment_count', 0) + 1
            elif (throttle < 0) or (now.timestamp() - aggregate.get('date_epoch', 0) < throttle):
                LOG.debug("Time within throttle {} range, discarding".format(throttle))
                self.db.write('record', record, update_time=False)
                raise Abort
            else:
                comment = {}
                comment['record_uid'] = aggregate['uid']
                comment['date'] = now.astimezone().isoformat()
                comment['auto'] = True
                if record.get('state') == 'close':
                    comment['message'] = 'Auto re-opened'
                    comment['type'] = 'open'
                    record['state'] = 'open'
                elif record.get('state') == 'ack':
                    comment['message'] = 'Auto re-escalated'
                    comment['type'] = 'esc'
                    record['state'] = 'esc'
                else:
                    comment['message'] = 'New escalation'
                    comment['type'] = 'comment'
                self.db.write('comment', comment)
                record['comment_count'] = aggregate.get('comment_count', 0) + 1
        else:
            LOG.debug("Not found, creating a new aggregate")
            record['duplicates'] = 1
        return record

    def reload_data(self, sync = False):
        super().reload_data()
        self.aggregate_rules = []
        for aggrule in (self.data or []):
            self.aggregate_rules.append(AggregateruleObject(aggrule))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class AggregateruleObject():
    def __init__(self, aggregate_rule):
        self.enabled = aggregate_rule.get('enabled', True)
        self.name = aggregate_rule['name']
        LOG.debug("Creating aggregate: {}".format(str(self.name)))
        self.condition = Condition(aggregate_rule.get('condition', ''))
        LOG.debug("-> condition: {}".format(str(self.condition)))
        self.fields = aggregate_rule.get('fields', [])
        self.fields.sort()
        self.watch = aggregate_rule.get('watch', [])
        try:
            self.throttle = int(aggregate_rule.get('throttle', 10))
        except:
            self.throttle = 10
    def match(self, record):
        """
        Check if a record matched this aggregate's rule condition

        Args:
            record (dict)

        Returns:
            bool: Record matched the rule's condition
        """
        LOG.debug("Attempting to match aggregate rule {} with record {}".format(str(self.name), str(record)))
        match = self.condition.match(record)
        if match:
            if not 'aggregate' in record:
                record['aggregate'] = self.name
        LOG.debug("-> Match result: {}".format(match))
        return match
    def process(self, record):
        """
        Process the record against this aggregate

        Args:
            record (dict)

        Returns:
            bool: Record has been modified
        """
        LOG.debug("Aggregate rule {} processing record: {}".format(str(self.name), str(record)))
        return self.match(record)
