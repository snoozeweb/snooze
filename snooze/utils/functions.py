#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module with some utils functions'''

import binascii
import os
import hashlib
from base64 import b64decode
from logging import getLogger
from pathlib import Path
from typing import Optional, List, Union, Any, TypeVar, Set, Tuple

import falcon
from falcon import Request, Response, HTTPError

from snooze.utils.typing import Record, AuthPayload

log = getLogger('snooze.utils.functions')

T = TypeVar('T')

def log_warning_handler(err: HTTPError, req: Request, _resp, _params):
    '''Log caught exceptions as a warning'''
    source = req.access_route[0]
    method = req.method
    path = req.relative_uri
    status = err.status[:3]
    message = f"{source} {method} {path} {status} - {err.description}"
    log.warning(message, exc_info=err)
    raise err

def log_error_handler(err: HTTPError, req: Request, _resp, _params):
    '''Log caught exceptions as an error'''
    source = req.access_route[0]
    method = req.method
    path = req.relative_uri
    status = err.status[:3]
    message = f"{source} {method} {path} {status} - {err.description}"
    log.error(message, exc_info=err)
    raise err

def log_ignore_handler(_err: HTTPError, _req: Request, _resp, _params):
    '''Do nothing'''

def log_uncaught_handler(err: Exception, _req, _resp, _params):
    '''Log uncaught exceptions and return a clean 5xx'''
    log.exception(err)
    raise falcon.HTTPInternalServerError(description=str(err)) from err

def unique(lst: list) -> list:
    '''Return a list with only unique elements'''
    return list(set(lst))

def dig(dic: dict, *lst: List[Union[str, int]]) -> Any:
    '''Like a Dict[value], but recursive'''
    if len(lst) > 0:
        try:
            if lst[0].isnumeric():
                return dig(dic[int(lst[0])], *lst[1:])
            else:
                return dig(dic[lst[0]], *lst[1:])
        except Exception:
            return None
    else:
        return dic

def ensure_kv(dic: dict, value: Any, *lst: list):
    '''Set value at dic[*lst]'''
    element = dic
    for i, raw_key in enumerate(lst):
        key = raw_key
        if raw_key.isnumeric():
            key = int(raw_key)
        try:
            if key not in element:
                if i == len(lst) - 1:
                    element[key] = value
                    return dic
                else:
                    element[key] = {}
            element = element[key]
        except Exception:
            return dic
    return dic

def sanitize(dic: T, str_from:str='.', str_to:str= '_') -> T:
    '''Sanitize a dict object keys to avoid issues with MongoDB
    (since MongoDB interpret dots)'''
    new_dic = {}
    if isinstance(dic, dict):
        for key, value in dic.items():
            new_dic[key.replace(str_from, str_to)] = sanitize(value)
        return new_dic
    else:
        return dic

def flatten(lst: list) -> list:
    '''Flatten a nested list'''
    return [z for y in lst for z in (flatten(y) if hasattr(y, '__iter__') and not isinstance(y, str) else (y,))]

def to_tuple(lst):
    '''Transform a nested list into a nested tuple'''
    return tuple(to_tuple(x) for x in lst) if isinstance(lst, list) else lst

CA_BUNDLE_PATHS = [
    '/etc/ssl/certs/ca-certificates.crt', # Debian / Ubuntu / Gentoo
    '/etc/pki/tls/certs/ca-bundle.crt', # RHEL 6
    '/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem', # RHEL 7
    '/etc/ssl/ca-bundle.pem', # OpenSUSE
    '/etc/pki/tls/cacert.pem', # OpenELEC
    '/etc/ssl/cert.pem', # Alpine Linux
]

def ca_bundle() -> Optional[str]:
    '''Returns Linux CA bundle path'''
    ssl_cert_file = os.environ.get('SSL_CERT_FILE')
    requests_ca_bundle = os.environ.get('REQUESTS_CA_BUNDLE')
    if ssl_cert_file is not None:
        return ssl_cert_file
    if requests_ca_bundle is not None:
        return requests_ca_bundle
    for ca_path in CA_BUNDLE_PATHS:
        if Path(ca_path).exists():
            return ca_path
    return None

def ensure_hash(record: Record):
    '''Given a record with a 'raw' key, compute the hash of the
    record if not present, and append it to the record'''
    if not 'hash' in record:
        if 'raw' in record:
            record['hash'] = hashlib.md5(record['raw']).hexdigest()
        else:
            record['hash'] = hashlib.md5(repr(sorted(record.items())).encode('utf-8')).hexdigest()

def is_authorized(route: 'BasicRoute', req: Request) -> bool:
    '''A wrapper function that check the authorization of a request'''
    if route.core.config.core.no_login:
        return True

    auth: AuthPayload = req.context.auth
    if auth.username == 'root' and auth.method == 'root':
        return True

    if route.options.check_permissions and req.method in ['PUT', 'POST', 'DELETE']:
        roles = route.get_roles(auth.username, auth.method)
        permissions = set(route.get_permissions(roles))
    else:
        permissions = auth.permissions

    if (route.plugin and hasattr(route.plugin, 'name')):
        plugin_name = route.plugin.name
    elif hasattr(route, 'name'):
        plugin_name = route.name
    else:
        plugin_name = None

    read_permissions: Set[str] = {'ro_all'}
    write_permissions: Set[str] = {'rw_all'}
    if plugin_name:
        read_permissions.add(f"ro_{plugin_name}")
        write_permissions.add(f"rw_{plugin_name}")

    authorization_policy = route.options.authorization_policy

    # Append authorizations to the permission set
    if authorization_policy:
        read_permissions |= authorization_policy.read
        write_permissions |= authorization_policy.write

    valid_permissions: Set[str] = set()
    if req.method in ['GET']:
        valid_permissions |= read_permissions | write_permissions
    elif req.method in ['PUT', 'POST', 'DELETE']:
        valid_permissions |= write_permissions

    auth_permissions = permissions | {'any'}

    return bool(auth_permissions & valid_permissions)

def authorize(callback):
    '''A decorator that inject the authorization context in the request'''
    def wrapper(route, req, resp, *args, **kwargs):
        if not is_authorized(route, req):
            raise falcon.HTTPForbidden(description=f"On {req.method} {req.path}, auth: {req.context.auth.dict()}")
        return callback(route, req, resp, *args, **kwargs)
    return wrapper

def extract_basic_auth(req: Request) -> Tuple[str, str]:
    '''Decode the user:password from an authorization header, as per the basic authentication'''
    authorization = req.get_header('Authorization')
    if authorization is None:
        raise falcon.HTTPMissingHeader(header_name='Authorization')
    try:
        scheme, credentials = authorization.split(' ', 1)
    except ValueError as err:
        raise falcon.HTTPInvalidHeader(header_name='Authorization',
            description='Must be in the form `Basic <credentials>`') from err
    if scheme != 'Basic':
        raise falcon.HTTPUnauthorized(description=f"Invalid authorization scheme: {scheme}."
            ' Must be `Basic`')
    try:
        result = b64decode(credentials).decode('utf-8')
        username, password = result.split(':', 1)
    except binascii.Error as err:
        raise falcon.HTTPInvalidHeader(header_name="Authorization",
            message=f"Authorization Basic not in base64: {err}") from err
    except UnicodeError as err:
        raise falcon.HTTPInvalidHeader(header_name="Authorization",
            message=f"Decoded Authorization Basic not unicode: {err}")
    except ValueError as err: # Cannot unpack username, password
        raise falcon.HTTPInvalidHeader(header_name='Authorization',
            message='Decoded entry should be in format `<user>:<password>`') from err

    return username, password
