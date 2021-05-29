#!/usr/bin/python3.6

from snooze.plugins.core import Plugin, Abort_and_write
from snooze.utils import Condition, Action

import time
import logging
import hashlib
from logging import getLogger
from copy import deepcopy
LOG = getLogger('snooze.process')

class Aggregaterule(Plugin):
    def process(self, record):
        """
        Process the record against a list of aggregate rules

        Args:
            record (dict)
        """
        LOG.debug("Processing record: {}".format(str(record)))
        for aggregate_rule in (self.data or []):
            aggrule = AggregateruleObject(aggregate_rule)
            if aggrule.process(record):
                agghash = hashlib.md5((str(aggrule.name) + '.'.join([(record.get(field) or '') for field in aggrule.fields])).encode()).hexdigest()
                LOG.debug("Checking if an aggregate with hash {} can be found".format(agghash))
                aggregate_result = self.db.search('aggregate', ['=', 'hash', agghash])
                if aggregate_result['count'] > 0:
                    aggregate = aggregate_result['data'][0]
                    LOG.debug("Found {}, updating it with the record infos".format(str(aggregate)))
                    uid = aggregate['uid']
                    rules = aggregate.get('rules') or []
                    aggregate.update(record)
                    aggregate['uid'] = uid
                    aggregate['hash'] = agghash
                    aggregate['rules'] = list(set(aggregate.get('rules') or []) | set(rules))
                    aggregate['duplicates'] += 1
                    if time.time() - aggregate['time_epoch'] < aggrule.throttle:
                        self.db.write('aggregate', aggregate, update_time=False)
                        raise Abort
                    else:
                        self.db.write('aggregate', aggregate)
                else:
                    LOG.debug("Not found, creating a new aggregate")
                    aggregate = deepcopy(record)
                    aggregate.pop('uid', None)
                    aggregate['duplicates'] = 1
                    aggregate['hash'] = agghash
                    self.db.write('aggregate', aggregate)
                break
        else:
            LOG.debug("Record {} could not match any aggregate rule, assigning a default aggregate".format(str(record)))
            aggregate = deepcopy(record)
            aggregate.pop('uid', None)
            aggregate['duplicates'] = 1
            aggregate['aggregate'] = 'default'
            self.db.write('aggregate', aggregate)

        return record

class AggregateruleObject():
    def __init__(self, aggregate_rule):
        self.name = aggregate_rule['name']
        LOG.debug("Creating aggregate: {}".format(str(self.name)))
        self.condition = Condition(aggregate_rule.get('condition', ''))
        LOG.debug("-> condition: {}".format(str(self.condition)))
        self.fields = aggregate_rule.get('fields') or []
        self.fields.sort()
        self.throttle = (aggregate_rule.get('throttle') or 0)
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
