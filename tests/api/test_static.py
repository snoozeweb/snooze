'''Static route testing'''

import falcon
import pytest
from falcon.testing import TestClient

from snooze.api import LoggerMiddleware
from snooze.api.routes import StaticRoute

@pytest.fixture(scope='function')
def client(tmp_path):
    api = falcon.App(middleware=[LoggerMiddleware()])
    api.add_route('/web', StaticRoute(tmp_path, prefix='/web'))
    (tmp_path / 'index.html').write_text('<!DOCTYPE html><html></html>\n')
    return TestClient(api)

class TestStaticRoute:
    def test_root(self, client):
        resp = client.simulate_get('/web')
        assert resp.status == '200 OK'
        assert resp.content == b'<!DOCTYPE html><html></html>\n'
    def test_404(self, client):
        resp = client.simulate_get('/web/unknown_resource.js')
        assert resp.status == '404 Not Found'
    def test_escape_root(self, client):
        '''We're supposed to catch it, but falcon resolve the `..` as well,
        and return a 404 for us.'''
        resp = client.simulate_get('/web/../../../etc/passwd')
        assert resp.status_code > 400
