#!/usr/bin/python3.6

from snooze.plugins.core import Plugin
from snooze.utils import Condition, Modification

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

    def process_rules(self, record, rules):
        LOG.debug("Processing record {} against rules: {}".format(str(record), str(rules)))
        for rule in rules:
            LOG.debug("Rule {} processing record: {}".format(str(self.name), str(record)))
            if rule.enabled and rule.match(record):
                rule.modify(record)
                self.process_rules(record, rule.children)

    def reload_data(self, sync = False):
        LOG.debug("Reloading data for plugin {}".format(self.name))
        self.data = self.db.search('rule', ['NOT', ['EXISTS', 'parent']], orderby='name')['data']
        self.rules = []
        for rule in (self.data or []):
            self.rules.append(RuleObject(rule, self.db))
        if sync and self.core.cluster:
            self.core.cluster.reload_plugin(self.name)

class RuleObject():
    def __init__(self, rule, db = None):
        self.enabled = rule.get('enabled', True)
        self.name = rule['name']
        LOG.debug("Creating rule: {}".format(str(self.name)))
        self.condition = Condition(rule.get('condition'))
        LOG.debug("-> condition: {}".format(str(self.condition)))
        self.modifications = []
        for modification in (rule.get('modifications') or []):
            LOG.debug("-> modification: {}".format(str(modification)))
            self.modifications.append(Modification(*modification))
        LOG.debug("Searching children of rule {}".format(str(self.name)))
        self.children = []
        if db:
            children = db.search('rule', ['=', 'parent', rule['uid']], orderby='name')['data']
            for child_rule in children:
                LOG.debug("Found child {} of rule {}".format(child_rule['name'], str(self.name)))
                self.children.append(RuleObject(child_rule, db))

    def match(self, record):
        """
        Check if a record matched this rule's condition

        Args:
            record (dict)

        Returns:
            bool: Record matched the rule's condition
        """
        LOG.debug("Attempting to match rule {} with record {}".format(str(self.name), str(record)))
        match = self.condition.match(record)
        if match:
            if not 'rules' in record:
                record['rules'] = []
            record['rules'].append(self.name)
        LOG.debug("-> Match result: {}".format(match))
        return match

    def modify(self, record):
        """
        Modify the record based of this rule's modifications

        Args:
            record (dict)

        Returns:
            bool: Record has been modified
        """
        LOG.debug("Attempting to modify record: {}".format(str(record)))
        modified = any([ modification.modify(record) for modification in self.modifications])
        if modified:
            LOG.debug("Record has been modified: {}".format(str(record)))
        else:
            LOG.debug("Record has not been modified")
        return modified

    def __repr__(self):
        return self.name
