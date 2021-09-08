#!/usr/bin/python3.6
import os
from pathlib import Path

def dig(dic, *lst):
    """
    Input: Dict, List
    Output: Any

    Like a Dict[value], but recursive
    """
    if len(lst) > 0:
        try:
            if lst[0].isnumeric():
                return dig(dic[int(lst[0])], *lst[1:])
            else:
                return dig(dic[lst[0]], *lst[1:])
        except:
            return None
    else:
        return dic

def sanitize(d, str_from = '.', str_to = '_'):
    new_d = {}
    if isinstance(d, dict):
        for k, v in d.items():
            new_d[k.replace(str_from, str_to)] = sanitize(v)
        return new_d
    else:
        return d

flatten = lambda x: [z for y in x for z in (flatten(y) if hasattr(y, '__iter__') and not isinstance(y, str) else (y,))]

def to_tuple(l):
    return tuple(to_tuple(x) for x in l) if type(l) is list else l

CA_BUNDLE_PATHS = [
    '/etc/ssl/certs/ca-certificates.crt', # Debian / Ubuntu / Gentoo
    '/etc/pki/tls/certs/ca-bundle.crt', # RHEL 6
    '/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem', # RHEL 7
    '/etc/ssl/ca-bundle.pem', # OpenSUSE
    '/etc/pki/tls/cacert.pem', # OpenELEC
    '/etc/ssl/cert.pem', # Alpine Linux
]

def ca_bundle():
    '''Returns Linux CA bundle path'''
    if os.environ.get('SSL_CERT_FILE'):
        return os.environ.get('SSL_CERT_FILE')
    elif os.environ.get('REQUESTS_CA_BUNDLE'):
        return os.environ.get('REQUESTS_CA_BUNDLE')
    else:
        for ca_path in CA_BUNDLE_PATHS:
            if Path(ca_path).exists():
                return ca_path
