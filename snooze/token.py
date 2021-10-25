#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing the token engine'''

import jwt

class TokenEngine:
    '''Sign and verify tokens'''
    def __init__(self, secret_key, algorithm='HS256'):
        self.secret = secret_key
        self.algorithm = algorithm

    def sign(self, payload):
        '''Sign a payload and return the token'''
        token = jwt.encode(payload, self.secret, algorithm=self.algorithm)
        return token

    def verify(self, token):
        '''Verify the token and return the payload'''
        payload = jwt.decode(token, self.secret, algorithm=[self.algorithm])
        return payload
