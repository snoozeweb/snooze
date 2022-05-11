#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

'''A module for managing the token engine'''

from datetime import datetime, timedelta

import falcon
import jwt
from falcon import Request, Response
from jwt.exceptions import InvalidTokenError
from pydantic import ValidationError

from snooze.utils.typing import AuthPayload

class TokenEngine:
    '''Sign and verify tokens'''
    def __init__(self, secret_key, algorithm='HS256'):
        self.secret = secret_key
        self.algorithm = algorithm

    def sign(self, payload: AuthPayload, lease: timedelta = timedelta(hours=1)) -> str:
        '''Sign a payload and return the token'''
        data = payload.dict()
        now = datetime.now().astimezone()
        data['exp'] = (now + lease).timestamp()
        data['nbf'] = now.timestamp()
        token = jwt.encode(data, self.secret, algorithm=self.algorithm)
        return token

    def verify(self, token: str) -> AuthPayload:
        '''Verify the token and return the payload'''
        data = jwt.decode(token, self.secret, algorithms=[self.algorithm], options={'require': ['exp', 'nbf']})
        return AuthPayload(**data)

class TokenAuthMiddleware:
    '''A falcon middleware for verifying JWT tokens'''

    def __init__(self, engine: TokenEngine):
        self.scheme = 'JWT'
        self.engine = engine

    def _process_request(self, req: Request) -> AuthPayload:
        '''Process a request which we need to verify the authentication.
        Return the authentication payload.'''
        authorization = req.get_header('Authorization')
        if authorization is None:
            raise falcon.HTTPMissingHeader(header_name='Authorization')
        try:
            scheme, credentials = authorization.split(' ', 1)
        except ValueError as err:
            raise falcon.HTTPInvalidHeader(header_name='Authorization',
                msg=f"Must be in the form `{self.scheme} <credentials>`") from err
        if scheme != self.scheme:
            raise falcon.HTTPUnauthorized(description=f"Invalid authorization scheme: {scheme}."
                f" Must be {self.scheme}")
        try:
            return self.engine.verify(credentials)
        except InvalidTokenError as err:
            raise falcon.HTTPUnauthorized(description=str(err)) from err
        except ValidationError as err:
            raise falcon.HTTPUnauthorized(
                description=f"Invalid payload found in JWT token: {err}") from err

    def process_resource(self, req: Request, _resp: Response, resource, *_args, **_kwargs):
        '''Method called for every request. Set the authentication payload in `req.context['auth']`'''
        # Handle CORS pre-flight requests case
        if req.method in ['OPTIONS']:
            return
        if getattr(resource, 'authentication', True):
            req.context.auth = self._process_request(req)
