#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python3.6

from snooze.plugins.core import Plugin
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.modification import get_modification, validate_modification

import logging
from logging import getLogger
LOG = getLogger('snooze.process')

class Rule(Plugin):
    def process(self, record):
        """
        Process the record against a list of rules

        Args:
            record (dict)
        """
        self.process_rules(record, self.rules)
        return record

    def validate(self, obj):
        '''Validate a rule object'''
        validate_condition(obj)
        validate_modification(obj, self.core)

    def process_rules(self, record, rules):
        LOG.debug("Processing record {} against rules".format(str(record.get('hash', ''))))
        for rule in rules:
            if rule.enabled and rule.match(record):
                LOG.debug("Rule {} matched record: {}".format(str(rule.name), str(record.get('hash', ''))))
                rule.modify(record)
                self.process_rules(record, rule.children)

    def reload_data(self, sync = False):
        LOG.debug("Reloading data for plugin {}".format(self.name))
        self.data = self.db.search('rule', ['NOT', ['EXISTS', 'parent']], orderby='name')['data']
        self.rules = []
        for rule in (self.data or []):
            self.rules.append(RuleObject(rule, self.core))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class RuleObject():
    def __init__(self, rule, core = None):
        self.enabled = rule.get('enabled', True)
        self.name = rule['name']
        LOG.debug("Creating rule: {}".format(str(self.name)))
        self.condition = get_condition(rule.get('condition'))
        LOG.debug("-> condition: {}".format(str(self.condition)))
        self.modifications = []
        for modification in (rule.get('modifications') or []):
            LOG.debug("-> modification: {}".format(str(modification)))
            self.modifications.append(get_modification(modification, core=core))
        LOG.debug("Searching children of rule {}".format(str(self.name)))
        self.children = []
        if core and core.db:
            db = core.db
            children = db.search('rule', ['=', 'parent', rule['uid']], orderby='name')['data']
            for child_rule in children:
                LOG.debug("Found child {} of rule {}".format(child_rule['name'], str(self.name)))
                self.children.append(RuleObject(child_rule, core))

    def match(self, record):
        """
        Check if a record matched this rule's condition

        Args:
            record (dict)

        Returns:
            bool: Record matched the rule's condition
        """
        match = self.condition.match(record)
        if match:
            if not 'rules' in record:
                record['rules'] = []
            if self.name not in record['rules']:
                record['rules'].append(self.name)
        return match

    def modify(self, record):
        """
        Modify the record based of this rule's modifications

        Args:
            record (dict)

        Returns:
            bool: Record has been modified
        """
        modified = False
        modifs = []
        for modification in self.modifications:
            if modification.modify(record):
                modified = True
                modifs.append(modification)
        if modified:
            LOG.debug("Record {} has been modified: {}".format(str(record.get('hash', '')), str([m.pprint() for m in modifs])))
        else:
            LOG.debug("Record {} has not been modified".format(str(record.get('hash', ''))))
        return modified

    def __repr__(self):
        return self.name
