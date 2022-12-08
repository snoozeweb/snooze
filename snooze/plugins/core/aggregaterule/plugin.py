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

from opentelemetry.trace import get_current_span

from snooze.plugins.core import Plugin, AbortAndUpdate, Abort
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.functions import dig

apilog = getLogger('snooze-api')
proclog = getLogger('snooze-process')

class Aggregaterule(Plugin):
    '''The aggregate rule plugin'''
    def process(self, record: dict):
        '''Process the record against a list of aggregate rules'''
        proclog.debug('Start')
        for aggrule in self.aggregate_rules:
            if aggrule.enabled and aggrule.match(record):
                record['hash'] = hashlib.md5((str(aggrule.name) + '.'.join([(field + '=' + (dig(record, *field.split('.')) or '')) for field in aggrule.fields])).encode()).hexdigest()
                proclog.info("Computed hash %s (from '%s')", record['hash'], self.name)
                record = self.match_aggregate(record, aggrule.throttle, aggrule.flapping, aggrule.watch, aggrule.name)
                if not isinstance(record, dict):
                    return record
                break
        else:
            proclog.debug('No aggregate rule matched, will use the default aggregate rule')
            if 'raw' in record:
                if isinstance(record['raw'], dict):
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'].items())).encode('utf-8')).hexdigest()
                elif isinstance(record['raw'], list):
                    record['hash'] = hashlib.md5(repr(sorted(record['raw'])).encode('utf-8')).hexdigest()
                else:
                    record['hash'] = hashlib.md5(record['raw'].encode('utf-8')).hexdigest()
            else:
                record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()
            proclog.info("Computed hash %s (from default aggregate rule)", record['hash'])
            record['aggregate'] = 'default'
            record = self.match_aggregate(record)
            if not isinstance(record, dict):
                return record

        # Adding useful information to the trace
        span = get_current_span()
        span.set_attribute('record_uid', record.get('uid', ''))
        span.set_attribute('aggregate_hash', record.get('hash', ''))

        proclog.debug('Done')
        return record

    def validate(self, obj):
        '''Validate an aggregate rule object'''
        validate_condition(obj)

    def match_aggregate(self, record, throttle=10, flapping=3, watch=[], aggrule_name='default'):
        '''Attempt to match an aggregate with a record, and throttle the record if it does'''
        proclog.debug("Checking in the db for hash %s", record['hash'])
        aggregate = self.db.get_one('record', dict(hash=record['hash']))
        if aggregate:
            proclog.debug("Found hash %s in db", record['hash'])
            now = datetime.datetime.now()
            record = dict(list(aggregate.items()) + list(record.items()))
            record_state = record.get('state', '')
            proclog.info("UID change to %s", aggregate['uid'])
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
                    proclog.info("OK received, closing alert")
                    aggregate_severity = aggregate.get('severity', 'unknown')
                    record_severity = record.get('severity', 'unknown')
                    comment['message'] = f"Auto closed: Severity {aggregate_severity} => {record_severity}"
                    comment['type'] = 'close'
                    record['state'] = 'close'
                    self.db.write('comment', comment)
                    record['comment_count'] = aggregate.get('comment_count', 0) + 1
                    return record
                else:
                    proclog.info("OK received but the alert is already closed, discarding")
                    return AbortAndUpdate(record=record)
            watched_fields = []
            for watched_field in watch:
                aggregate_field = dig(aggregate, *watched_field.split('.'))
                record_field = dig(record, *watched_field.split('.'))
                if record_field != aggregate_field:
                    proclog.info("The watch field '%s' changed: %s -> %s", watched_field, aggregate_field, record_field)
                    watched_fields.append({'name': watched_field, 'old': aggregate_field, 'new': record_field})
            if watched_fields:
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
                proclog.debug("Issued '%s' comment because of watch field change", comment['type'])
                record['flapping_countdown'] = aggregate.get('flapping_countdown', flapping) - 1
            elif record.get('state') == 'close':
                proclog.info("Auto-reopening the aggregate %s", record['hash'])
                comment['message'] = 'Auto re-opened'
                comment['type'] = 'open'
                record['state'] = 'open'
                record['flapping_countdown'] = aggregate.get('flapping_countdown', flapping) - 1
            elif throttling:
                proclog.info("Alert throttled (time within %s range), discarding", throttle)
                self.core.stats.inc('alert_throttled', {'name': aggrule_name})
                return AbortAndUpdate(record=record)
            else:
                if record.get('state') == 'ack':
                    comment['type'] = 'esc'
                    record['state'] = 'esc'
                comment['message'] = 'New escalation'
            flapping_count = record.get('flapping_countdown', 1)
            if flapping_count == 0:
                seconds_left = throttle - (now.timestamp() - aggregate.get('date_epoch', 0))
                proclog.info("Flapping detected. Stopped notification until throlle expires (%d sec left)", seconds_left)
                flapping_message = f"Flapping detected. Stopped notifications until throttle expires ({seconds_left}s left)"
                if 'message' in comment:
                    comment['message'] += '\n' + flapping_message
                else:
                    comment['message'] = flapping_message
            if 'message' in comment:
                self.db.write('comment', comment)
            record['comment_count'] = aggregate.get('comment_count', 0) + 1
            if flapping_count <= 0:
                proclog.info("Alert is flapping, discarding")
                return AbortAndUpdate(record=record)
        else:
            proclog.info("Creating aggregate %s", record['hash'])
            matched = self.db.replace_one('aggregate', {'hash': record['hash']}, record, update_time=False)
            if matched > 0:
                proclog.warning("Received 2 alerts with same hash %s at the same time. Discarding one", record['hash'])
                return Abort()
            record['duplicates'] = 1
        record.pop('snoozed', '')
        record.pop('notifications', '')
        return record

    def _post_reload(self):
        aggregate_rules = []
        for aggrule in (self.data or []):
            aggregate_rules.append(AggregateruleObject(aggrule))
        self.aggregate_rules = aggregate_rules

class AggregateruleObject:
    def __init__(self, aggregate_rule):
        self.enabled = aggregate_rule.get('enabled', True)
        self.name = aggregate_rule['name']
        apilog.debug("Instantiating aggregate rule: %s", self.name)
        self.condition = get_condition(aggregate_rule.get('condition', ''))
        apilog.debug("Instantiating condition: %s", self.condition)
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
    def match(self, record: dict) -> bool:
        '''Check if a record matched this aggregate's rule condition'''
        match = self.condition.match(record)
        if match:
            if not 'aggregate' in record:
                record['aggregate'] = self.name
        return match
