#!/usr/bin/python36

from bson.json_util import dumps
from jinja2 import Template
from bson.json_util import loads, dumps
from urllib.parse import unquote
import requests

from snooze.plugins.action import Action

from logging import getLogger
log = getLogger('snooze.action.script')


class Webhook(Action):
    def __init__(self, core):
        super().__init__(core)

    def pprint(self, content):
        output = content.get('url')
        params = content.get('params')
        payload = content.get('payload')
        if params:
            output += ' data=[' + ', '.join(map(lambda x: ': '.join(x), params))+']'
        if payload:
            output += ' payload=' + payload
        return output

    def send(self, record, content):
        url = content.get('url', '')
        params = content.get('params', [])
        payload = content.get('payload')
        proxy = content.get('proxy')
        inject_response = content.get('inject_response', False)
        if payload:
            try:
                payload_list = loads(unquote(payload))
                parsed_payload = interpret_jinja_dict(payload_list, record)
                log.debug("Parsed payload: {}".format(parsed_payload))
            except Exception as e:
                log.exception(e)
                parsed_payload = None
        else:
            parsed_payload = None
        parsed_params = []
        for argument in params:
            if type(argument) is str:
                parsed_params += [interpret_jinja([argument, ''], record)]
            if type(argument) is list:
                parsed_params += [interpret_jinja(argument, record)]
            if type(argument) is dict:
                parsed_params += [sum([interpret_jinja([k, v], record) for k, v in argument])]
        log.debug("Will execute action webhook `{}`".format(url))
        if str.startswith(url, 'https') and content.get('ssl_verify'): 
            ssl_verify=True
        else:
            ssl_verify=False
        response = None
        try:
            if parsed_params:
                parsed_params = { parsed_params[i][0]: parsed_params[i][1] for i in range(0, len(parsed_params)) }
                log.debug("Parsed params: {}".format(parsed_params))
            else:
                parsed_params = None
            response = RestHelper().send_http_request(url, 'POST', payload=parsed_payload, parameters=parsed_params, verify=ssl_verify, proxy_uri=proxy)
            log.debug("HTTP Response: {}".format(response))
        except Exception as e:
            log.exception(e)
        if inject_response and response and response.status_code == 200:
            try:
                response_content = loads(response.content)
            except:
                response_content = response.content
            log.debug(content)
            record['response_' + content.get('action_name', self.name).replace(' ', '_')] = response_content

def interpret_jinja(fields, record):
    return list(map(lambda field: Template(field).render(record), fields))

def interpret_jinja_dict(d, record):
    a = {}
    for k, v in d.items():
        if isinstance(v, dict):
            a[Template(k).render(record)] = interpret_jinja_dict(v, record)
        else:
            a[Template(k).render(record)] = Template(v).render(record)
    return a

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
        requests_args = {'timeout': (10.0, 5.0), 'verify': verify}
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

