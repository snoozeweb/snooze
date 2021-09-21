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
