#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''Plugin for aggregating records by a group of keys, thus avoiding users
from getting spammed for the same alert'''

import datetime
import hashlib
from logging import getLogger

from snooze.plugins.core import Plugin, AbortAndUpdate, Abort
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.functions import dig

log = getLogger('snooze.aggregaterule')


class Aggregaterule(Plugin):
    '''The aggregate rule plugin'''
    def process(self, record):
        """Process the record against a list of aggregate rules

        Args:
            record (dict)
        """
        log.debug("Processing record against aggregate rules")
        for aggrule in self.aggregate_rules:
            if aggrule.enabled and aggrule.match(record):
                record['hash'] = hashlib.md5((str(aggrule.name) + '.'.join([(field + '=' + (dig(record, *field.split('.')) or '')) for field in aggrule.fields])).encode()).hexdigest()
                log.debug("Aggregate rule %s matched record: %s", aggrule.name, record['hash'])
                record = self.match_aggregate(record, aggrule.throttle, aggrule.flapping, aggrule.watch, aggrule.name)
                break
        else:
            log.debug("Record %s could not match any aggregate rule, assigning a default aggregate", record)
            if 'raw' in record:
                if isinstance(record['raw'], dict):
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'].items())).encode('utf-8')).hexdigest()
                elif isinstance(record['raw'], list):
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'])).encode('utf-8')).hexdigest()
                else:
                    record['hash'] = hashlib.md5(record['raw'].encode('utf-8')).hexdigest()
            else:
                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
            record['aggregate'] = 'default'
            record = self.match_aggregate(record)

        return record

    def validate(self, obj):
        '''Validate an aggregate rule object'''
        validate_condition(obj)

    def match_aggregate(self, record, throttle=10, flapping=3, watch=[], aggrule_name='default'):
        '''Attempt to match an aggregate with a record, and throttle the record if it does'''
        log.debug("Checking if an aggregate with hash %s can be found", record['hash'])
        aggregate = self.db.get_one('record', dict(hash=record['hash']))
        if aggregate:
            log.debug("Found record hash %s, updating it with the record infos", record['hash'])
            now = datetime.datetime.now()
            record = dict(list(aggregate.items()) + list(record.items()))
            record_state = record.get('state', '')
            record['uid'] = aggregate.get('uid', '')
            record['state'] = aggregate.get('state', '')
            record['duplicates'] = aggregate.get('duplicates', 0) + 1
            record['date_epoch'] = aggregate.get('date_epoch', now.timestamp())
            record.pop('notification_from', '')
            if aggregate.get('ttl', -1) < 0:
                record['ttl'] = aggregate.get('ttl', -1)
            comment = {}
            comment['record_uid'] = aggregate.get('uid')
            comment['date'] = now.astimezone().isoformat()
            comment['auto'] = True
            comment['type'] = 'comment'
            throttling = (throttle < 0) or (now.timestamp() - aggregate.get('date_epoch', 0) < throttle)
            if not throttling:
                record.pop('flapping_countdown', '')
            if record_state == 'close':
                self.core.stats.inc('alert_closed', {'name': aggrule_name})
                if record.get('state') != 'close':
                    log.debug("OK received, closing alert")
                    aggregate_severity = aggregate.get('severity', 'unknown')
                    record_severity = record.get('severity', 'unknown')
                    comment['message'] = f"Auto closed: Severity {aggregate_severity} => {record_severity}"
                    comment['type'] = 'close'
                    record['state'] = 'close'
                    self.db.write('comment', comment)
                    record['comment_count'] = aggregate.get('comment_count', 0) + 1
                    return record
                else:
                    log.debug("OK received but the alert is already closed, discarding")
                    raise AbortAndUpdate(record)
            watched_fields = []
            for watched_field in watch:
                aggregate_field = dig(aggregate, *watched_field.split('.'))
                record_field = dig(record, *watched_field.split('.'))
                log.debug("Watched field %s: compare %s and %s", watched_field, record_field, aggregate_field)
                if record_field != aggregate_field:
                    watched_fields.append({'name': watched_field, 'old': aggregate_field, 'new': record_field})
            if watched_fields:
                log.debug("Alert %s Found updated fields from watchlist: %s", str(record['hash']), watched_fields)
                append_txt = []
                for watch_field in watched_fields:
                    append_txt.append("{watch_field['name']} ({watch_field['old']} => {watch_field['new']})")
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
                record['flapping_countdown'] = aggregate.get('flapping_countdown', flapping) - 1
            elif record.get('state') == 'close':
                comment['message'] = 'Auto re-opened'
                comment['type'] = 'open'
                record['state'] = 'open'
                record['flapping_countdown'] = aggregate.get('flapping_countdown', flapping) - 1
            elif throttling:
                log.debug("Alert %s Time within throttle %s range, discarding", record['hash'], throttle)
                self.core.stats.inc('alert_throttled', {'name': aggrule_name})
                raise AbortAndUpdate(record)
            else:
                if record.get('state') == 'ack':
                    comment['type'] = 'esc'
                    record['state'] = 'esc'
                comment['message'] = 'New escalation'
            flapping_count = record.get('flapping_countdown', 1)
            if flapping_count == 0:
                flapping_message = "Flapping detected. Stopped notifications until throttle expires ({}s left)".format(
                    throttle - (now.timestamp() - aggregate.get('date_epoch', 0)))
                if 'message' in comment:
                    comment['message'] += '\n' + flapping_message
                else:
                    comment['message'] = flapping_message
            if 'message' in comment:
                self.db.write('comment', comment)
            record['comment_count'] = aggregate.get('comment_count', 0) + 1
            if flapping_count <= 0:
                log.debug("Alert %s is flapping, discarding", record['hash'])
                raise AbortAndUpdate(record)
        else:
            matched = self.db.replace_one('aggregate', {'hash': record['hash']}, record, update_time=False)
            if matched > 0:
                log.debug("Received 2 alerts with same hash %s at the same time. Discarding one", record['hash'])
                raise Abort()
            log.debug("Not found, creating a new aggregate")
            record['duplicates'] = 1
        record.pop('snoozed', '')
        record.pop('notifications', '')
        return record

    def reload_data(self, sync = False):
        super().reload_data()
        aggregate_rules = []
        for aggrule in (self.data or []):
            aggregate_rules.append(AggregateruleObject(aggrule))
        self.aggregate_rules = aggregate_rules
        if sync:
            self.sync_neighbors()

class AggregateruleObject:
    def __init__(self, aggregate_rule):
        self.enabled = aggregate_rule.get('enabled', True)
        self.name = aggregate_rule['name']
        log.debug("Creating aggregate: %s", self.name)
        self.condition = get_condition(aggregate_rule.get('condition', ''))
        log.debug("-> condition: %s", self.condition)
        self.fields = aggregate_rule.get('fields', [])
        self.fields.sort()
        self.watch = aggregate_rule.get('watch', [])
        try:
            self.throttle = int(aggregate_rule.get('throttle', 10))
        except (ValueError, TypeError):
            self.throttle = 10
        try:
            self.flapping = int(aggregate_rule.get('flapping', 3))
        except (ValueError, TypeError):
            self.flapping = 3
    def match(self, record):
        """Check if a record matched this aggregate's rule condition

        Args:
            record (dict)

        Returns:
            bool: Record matched the rule's condition
        """
        match = self.condition.match(record)
        if match:
            if not 'aggregate' in record:
                record['aggregate'] = self.name
        return match
