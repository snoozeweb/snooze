#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

from urllib.parse import unquote
from copy import deepcopy
from logging import getLogger

import requests
import bson.json_util
from jinja2 import Template, Environment, BaseLoader

from snooze.plugins.core import Plugin
from snooze.utils.functions import ca_bundle

log = getLogger('snooze.action.script')

class Webhook(Plugin):
    def __init__(self, core):
        super().__init__(core)
        self.ca_bundle = ca_bundle()

    def pprint(self, content):
        output = content.get('url')
        params = content.get('params')
        payload = content.get('payload')
        if params:
            output += ' data=[' + ', '.join(map(lambda x: ': '.join(x), params))+']'
        if payload:
            output += ' payload=' + payload
        return output

    def send(self, records, content):
        if not isinstance(records, list):
            records = [records]
        url = content.get('url', '')
        params = content.get('params', [])
        payload = content.get('payload')
        proxy = content.get('proxy')
        action_name = content.get('action_name', self.name)
        inject_response = content.get('inject_response', False)
        batch = content.get('batch', False)
        to_send = []
        for record in records:
            record_copy = deepcopy(record)
            record_copy['__self__'] = record_copy.copy()
            to_send.append({'record': record, 'record_copy': record_copy, 'parsed_payload': None, 'response': None})
        if payload:
            unquoted_payload = unquote(payload)
            log.debug("Unquoted payload: %s", unquoted_payload)
            env = Environment(loader=BaseLoader)
            env.policies['json.dumps_kwargs'] = {'default': str}
            for artifact in to_send:
                try:
                    payload_jinja = env.from_string(unquoted_payload).render(artifact['record_copy'])
                    log.debug("Jinja payload for %s: %s", artifact['record_copy'].get('hash', ''), payload_jinja)
                    parsed_payload = bson.json_util.loads(payload_jinja)
                    artifact['parsed_payload'] = parsed_payload
                except Exception as err:
                    log.exception(err)
        parsed_params = [['snooze_action_name', action_name]]
        for artifact in to_send:
            try:
                for argument in params:
                    if isinstance(argument, str):
                        parsed_params += [interpret_jinja([argument, ''], artifact['record_copy'])]
                    elif isinstance(argument, list):
                        parsed_params += [interpret_jinja(argument, artifact['record_copy'])]
                    elif isinstance(argument, dict):
                        parsed_params += [sum([interpret_jinja([k, v], artifact['record_copy']) for k, v in argument])]
            except Exception as err:
                log.exception(err)
        params_dict = {}
        for i in range(len(parsed_params)):
            params_dict[parsed_params[i][0]] = parsed_params[i][1]
        log.debug("Parsed params: %s", params_dict)
        log.debug("Will execute action webhook `%s`", url)
        if str.startswith(url, 'https') and content.get('ssl_verify'):
            ssl_verify = self.ca_bundle
        else:
            ssl_verify = False
        if batch:
            response = None
            response_content_json = None
            try:
                response = RestHelper().send_http_request(url, 'POST', payload=[artifact['parsed_payload'] for artifact in to_send if artifact['parsed_payload']], parameters=params_dict, verify=ssl_verify, proxy_uri=proxy)
            except Exception as err:
                log.exception(err)
                response = None
            try:
                response_content_json = bson.json_util.loads(response.content)
            except Exception as err:
                log.exception(err)
                response_content_json = None
            for artifact in to_send:
                artifact['response'] = response
                if response_content_json:
                    try:
                        artifact['response_content'] = response_content_json[artifact['record']['hash']]
                    except Exception:
                        artifact['response_content'] = response_content_json
        else:
            for artifact in to_send:
                try:
                    artifact['response'] = RestHelper().send_http_request(url, 'POST', payload=artifact['parsed_payload'], parameters=parsed_params, verify=ssl_verify, proxy_uri=proxy)
                except Exception as err:
                    log.exception(err)
                    artifact['response'] = None
                if artifact['response']:
                    try:
                        artifact['response_content'] = bson.json_util.loads(artifact['response'].content)
                    except Exception as err:
                        log.exception(err)
                        artifact['response_content'] = None
        succeeded = []
        failed = []
        for artifact in to_send:
            log.debug("HTTP Response: %s", artifact.get('response', ''))
            if artifact['response'] and artifact['response'].status_code == 200:
                if inject_response:
                    response_dict = {'action_name': action_name, 'content': artifact.get('response_content', {})}
                    if 'snooze_webhook_responses' not in artifact['record']:
                        artifact['record']['snooze_webhook_responses'] = []
                    for idx, action_response in enumerate(artifact['record']['snooze_webhook_responses']):
                        if action_response.get('action_name') == action_name:
                            artifact['record']['snooze_webhook_responses'][idx] = response_dict
                            break
                    else:
                        artifact['record']['snooze_webhook_responses'].append(response_dict)
                succeeded.append(artifact['record'])
            else:
                failed.append(artifact['record'])
        return succeeded, failed

def interpret_jinja(fields, record):
    return list(map(lambda field: Template(field).render(record), fields))

def interpret_jinja_dict(dic, record):
    new_dic = {}
    for key, value in dic.items():
        if isinstance(value, dict):
            new_dic[Template(key).render(record)] = interpret_jinja_dict(value, record)
        else:
            new_dic[Template(key).render(record)] = Template(value).render(record)
    return new_dic

class RestHelper:
    def __init__(self):
        self.http_session = None
        self.requests_proxy = None

    def _init_request_session(self, proxy_uri=None):
        self.http_session = requests.Session()
        self.http_session.mount(
            'http://', requests.adapters.HTTPAdapter(max_retries=3))
        self.http_session.mount(
            'https://', requests.adapters.HTTPAdapter(max_retries=3))
        if proxy_uri:
            self.requests_proxy = {'http': proxy_uri, 'https': proxy_uri}

    def send_http_request(self, url, method, parameters=None, payload=None, headers=None, cookies=None, verify=True,
                          cert=None, timeout=None, proxy_uri=None):
        if self.http_session is None:
            self._init_request_session(proxy_uri)
        requests_args = {'timeout': (10.0, 13.0), 'verify': verify}
        if parameters:
            requests_args['params'] = parameters
        if payload:
            if isinstance(payload, (dict, list)):
                requests_args['json'] = payload
            else:
                requests_args['data'] = str(payload)
        if headers:
            requests_args['headers'] = headers
        if cookies:
            requests_args['cookies'] = cookies
        if cert:
            requests_args['cert'] = cert
        if timeout is not None:
            requests_args['timeout'] = timeout
        if self.requests_proxy:
            requests_args['proxies'] = self.requests_proxy
        return self.http_session.request(method, url, **requests_args)

