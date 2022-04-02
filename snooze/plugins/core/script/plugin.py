#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python36

import bson.json_util
from subprocess import run, CalledProcessError, PIPE
from jinja2 import Template
from copy import deepcopy

from snooze.plugins.core import Plugin

from logging import getLogger
log = getLogger('snooze.action.script')

class Script(Plugin):
    def __init__(self, core):
        super().__init__(core)

    def pprint(self, content):
        output = content.get('script')
        arguments = content.get('arguments')
        if arguments:
            output += ' ' + ' '.join(map(lambda x: ' '.join(x), arguments))
        return output

    def send(self, records, content):
        if not isinstance(records, list):
            records = [records]
        script = [content.get('script', '')]
        arguments = content.get('arguments', [])
        json = content.get('json', False)
        batch = content.get('batch', False)
        succeeded = []
        failed = []
        records_copy = []
        for record in records:
            record_copy = deepcopy(record)
            record_copy['__self__'] = record_copy.copy()
            records_copy.append(record_copy)
        artifacts = [{'arguments': [], 'record': {'alerts': records_copy}, 'json': None}]
        if not batch:
            artifacts = [{'arguments': [], 'record': record, 'json': None} for record in records_copy]
        for artifact in artifacts:
            try:
                for argument in arguments:
                    if type(argument) is str:
                        artifact['arguments'] += interpret_jinja([argument], artifact['record'])
                    elif type(argument) is list:
                        artifact['arguments'] += interpret_jinja(argument, artifact['record'])
                    elif type(argument) is dict:
                        artifact['arguments'] += sum([interpret_jinja([k, v], artifact['record']) for k, v in argument])
                succeeded.append(artifact)
            except Exception as e:
                log.exception(e)
                failed.append(artifact)
        log.debug("Will execute action script `{}`".format(' '.join(script)))
        if json:
            tmp_succeeded = []
            for artifact in succeeded:
                try:
                    artifact['json'] = bson.json_util.dumps(artifact['record'])
                    tmp_succeeded.append(artifact)
                except Exception as e:
                    log.exception(e)
                    failed.append(artifact)
            succeeded = tmp_succeeded
        tmp_succeeded = []
        for artifact in succeeded:
            try:
                process = run(script + artifact['arguments'], stdout=PIPE, input=artifact['json'], encoding='ascii')
                tmp_succeeded.append(artifact)
                log.debug('stdout: ' + str(process.stdout))
                log.debug('stderr: ' + str(process.stderr))
            except Exception as e:
                log.exception(e)
                failed.append(artifact)
        succeeded = tmp_succeeded
        if batch:
            if failed:
                failed = failed[0]['record']['alerts']
            if succeeded:
                succeeded = succeeded[0]['record']['alerts']
        else:
            failed = [artifact['record'] for artifact in failed]
            succeeded = [artifact['record'] for artifact in succeeded]
        return succeeded, failed

def interpret_jinja(fields, record):
    return list(map(lambda field: Template(field).render(record), fields))
