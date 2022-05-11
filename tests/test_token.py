'''Test of token engine and middleware'''

from datetime import datetime, timedelta

import jwt
import falcon
import pytest
from falcon.testing import TestClient

from snooze.token import TokenEngine, TokenAuthMiddleware
from snooze.utils.typing import AuthPayload

class TestRoute:
    def on_get(self, req, resp):
        resp.media = req.context['auth'].dict()

@pytest.fixture(scope='function')
def client():
    secret = 'secret123'
    engine = TokenEngine(secret, algorithm='HS256')
    app = falcon.App(middleware=[TokenAuthMiddleware(engine)])
    app.add_route('/test', TestRoute())
    return TestClient(app)

class TestTokenEngine:
    def test_sign(self):
        secret = 'secret123'
        engine = TokenEngine(secret, algorithm='HS256')
        auth = AuthPayload(username='test', method='local')
        token = engine.sign(auth)

        assert isinstance(token, str)
        payload = jwt.decode(token, secret, algorithms=['HS256'])
        now = datetime.now()
        assert payload['username'] == 'test'
        assert payload['method'] == 'local'
        assert payload['exp'] > datetime.now().timestamp()

    def test_verify(self):
        secret = 'secret123'
        engine = TokenEngine(secret, algorithm='HS256')
        now = datetime.now()
        payload = {
            'username': 'test',
            'method': 'local',
            'exp': (now + timedelta(hours=1)).timestamp(),
            'nbf': now.timestamp(),
        }
        token = jwt.encode(payload, secret, algorithm='HS256')
        auth = engine.verify(token)
        assert auth.username == 'test'
        assert auth.method == 'local'

class TestTokenAuthMiddleware:
    def test_process_resource(self, client):
        secret = 'secret123'
        now = datetime.now()
        payload = {
            'username': 'test',
            'method': 'local',
            'exp': (now + timedelta(hours=1)).timestamp(),
            'nbf': now.timestamp(),
        }
        token = jwt.encode(payload, secret, algorithm='HS256')
        headers = {'Authorization': f"JWT {token}"}
        resp = client.simulate_get('/test', headers=headers)
        assert resp.status == '200 OK'
        assert resp.json['username'] == 'test'
        assert resp.json['method'] == 'local'

    def test_missing_header(self, client):
        resp = client.simulate_get('/test')
        assert resp.status == '400 Bad Request'

    def test_wrong_secret(self, client):
        secret = 'secret456'
        payload = {'username': 'test', 'method': 'local'}
        token = jwt.encode(payload, secret, algorithm='HS256')
        headers = {'Authorization': f"JWT {token}"}
        resp = client.simulate_get('/test', headers=headers)
        assert resp.status == '401 Unauthorized'

    def test_broken_credentials(self, client):
        headers = {'Authorization': "completely-wrong"}
        resp = client.simulate_get('/test', headers=headers)
        assert resp.status == '400 Bad Request'

    def test_wrong_scheme(self, client):
        headers = {'Authorization': "Basic dGVzdDpwYXNzd29yZA=="}
        resp = client.simulate_get('/test', headers=headers)
        assert resp.status == '401 Unauthorized'

    def test_wrong_payload(self, client):
        secret = 'secret123'
        now = datetime.now()
        payload = { # Wrong Payload
            'username123': 'test',
            'method456': 'local',
            'exp': (now + timedelta(hours=1)).timestamp(),
            'nbf': now.timestamp(),
        }
        token = jwt.encode(payload, secret, algorithm='HS256')
        headers = {'Authorization': f"JWT {token}"}
        resp = client.simulate_get('/test', headers=headers)
        assert resp.status == '401 Unauthorized'
