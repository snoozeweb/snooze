#!/usr/bin/python3.6

from snooze.plugins.core import Plugin
from snooze.utils import Condition, Action

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
        LOG.debug("Processing record: {}".format(str(record)))
        self.data = self.db.search('rule', ['NOT', ['EXISTS', 'parent']], orderby='name')['data']
        for rule in (self.data or []):
            if RuleObject(rule).process(record):
                children = self.db.search('rule', ['=', 'parent', rule['uid']], orderby='name')['data']
                for child_rule in children:
                    LOG.debug("-> subrule of {}".format(str(child_rule['name'])))
                    RuleObject(child_rule).process(record)
        return record

class RuleObject():
    def __init__(self, rule):
        self.name = rule['name']
        LOG.debug("Creating rule: {}".format(str(self.name)))
        self.condition = Condition(rule.get('condition'))
        LOG.debug("-> condition: {}".format(str(self.condition)))
        self.actions = []
        for action in (rule.get('actions') or []):
            LOG.debug("-> action: {}".format(str(action)))
            self.actions.append(Action(*action))
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
        Modify the record based of this rule's actions

        Args:
            record (dict)

        Returns:
            bool: Record has been modified
        """
        LOG.debug("Attempting to modify record: {}".format(str(record)))
        modified = any([ action.modify(record) for action in self.actions])
        if modified:
            LOG.debug("Record has been modified: {}".format(str(record)))
        else:
            LOG.debug("Record has not been modified")
        return modified
    def process(self, record):
        """
        Process the record against this rule

        Args:
            record (dict)

        Returns:
            bool: Record has been modified
        """
        LOG.debug("Rule {} processing record: {}".format(str(self.name), str(record)))
        modified = False
        if self.match(record):
            # The record reference is the same, the content is modified
            modified = self.modify(record)
        return modified
