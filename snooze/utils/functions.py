#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module with some utils functions'''

import os
import hashlib
from pathlib import Path
from typing import Optional, List, Union, Any, TypeVar

from snooze.utils.typing import Record

T = TypeVar('T')

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
