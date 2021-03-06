#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A plugin for defining rules to apply modifications on records matching the
rule's condition'''

from logging import getLogger
from typing import List

from snooze.plugins.core import Plugin
from snooze.utils.condition import get_condition, validate_condition
from snooze.utils.modification import get_modification, validate_modification
from snooze.utils.typing import Record, Rule as RuleType

log = getLogger('snooze.process')

class Rule(Plugin):
    '''The rule plugin main class'''
    def process(self, record):
        '''Process the record against a list of rules'''
        self.process_rules(record, self.rules)
        return record

    def validate(self, obj):
        '''Validate a rule object'''
        validate_condition(obj)
        validate_modification(obj, self.core)

    def process_rules(self, record, rules):
        '''Process a list of rules'''
        log.debug("Processing record %s against rules", record.get('hash', ''))
        for rule in rules:
            if rule.enabled and rule.match(record):
                log.debug("Rule %s matched record: %s", rule.name, record.get('hash', ''))
                rule.modify(record)
                self.process_rules(record, rule.children)

    def reload_data(self, sync = False):
        log.debug("Reloading data for plugin %s", self.name)
        self.data = self.db.search('rule', ['NOT', ['EXISTS', 'parent']], orderby=self.meta.force_order)['data']
        rules = []
        for rule in (self.data or []):
            rules.append(RuleObject(rule, self))
        self.rules = rules
        if sync:
            self.sync_neighbors()

class RuleObject:
    '''An object representing the rule object in the database'''
    def __init__(self, rule: RuleType, rule_plugin: Rule = None):
        core = None
        order = None
        if rule_plugin:
            core = rule_plugin.core
            order = rule_plugin.meta.force_order
        self.enabled = rule.get('enabled', True)
        self.name = rule['name']
        log.debug("Creating rule: %s", self.name)
        self.condition = get_condition(rule.get('condition'))
        log.debug("-> condition: %s", self.condition)
        self.modifications = []
        for modification in (rule.get('modifications') or []):
            log.debug("-> modification: %s", modification)
            self.modifications.append(get_modification(modification, core=core))
        log.debug("Searching children of rule %s", self.name)
        self.children = []
        if core and core.db:
            db = core.db
            children = db.search('rule', ['=', 'parent', rule['uid']], orderby=order)['data']
            for child_rule in children:
                log.debug("Found child %s of rule %s", child_rule['name'], self.name)
                self.children.append(RuleObject(child_rule, rule_plugin))

    def match(self, record: Record) -> bool:
        '''Check if a record matched this rule's condition'''
        match = self.condition.match(record)
        if match:
            if not 'rules' in record:
                record['rules'] = []
            if self.name not in record['rules']:
                record['rules'].append(self.name)
        return match

    def modify(self, record: Record) -> bool:
        '''Modify the record based of this rule's modifications'''
        modified = False
        modifs = []
        for modification in self.modifications:
            if modification.modify(record):
                modified = True
                modifs.append(modification)
        if modified:
            log.debug("Record %s has been modified: %s", record.get('hash', ''), [m.pprint() for m in modifs])
        else:
            log.debug("Record %s has not been modified", record.get('hash', ''))
        return modified

    def __repr__(self):
        return self.name
