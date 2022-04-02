'''Audit logs related tests'''


class TestAudit:

    data = {
        'rule': [],
        'audit': [],
    }

    def test_create(self, client):
        rule = {'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]]}
        added = client.simulate_post('/api/rule', json=rule).json['data']['added']
        assert len(added) == 1
        rule_uid = added[0]['uid']

        audits = client.simulate_get('/api/audit').json['data']
        assert len(audits) == 1
        assert audits[0]['collection'] == 'rule'
        assert audits[0]['object_id'] == rule_uid
        assert audits[0]['action'] == 'added'
        assert audits[0]['snapshot'].items() >= rule.items()

    def test_create_multiple(self, client):
        rules = [
            {'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]]},
            {'name': 'rule02', 'condition': ['=', 'b', 'y'], 'modifications': [['SET', 'x', 2]]},
        ]
        added = client.simulate_post('/api/rule', json=rules).json['data']['added']
        assert len(added) == 2

        audits = client.simulate_get('/api/audit').json['data']
        assert len(audits) == 2

        assert audits[0]['collection'] == 'rule'
        assert audits[0]['object_id'] == added[0]['uid']
        assert audits[0]['action'] == 'added'
        assert audits[0]['snapshot'].items() >= rules[0].items()

        assert audits[1]['collection'] == 'rule'
        assert audits[1]['object_id'] == added[1]['uid']
        assert audits[1]['action'] == 'added'
        assert audits[1]['snapshot'].items() >= rules[1].items()

    def test_update(self, client):
        rule = {'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]]}
        added = client.simulate_post('/api/rule', json=rule).json['data']['added']
        assert len(added) == 1
        rule_uid = added[0]['uid']

        rule_update = {'uid': rule_uid, 'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]], 'comment': 'my comment'}
        updated = client.simulate_put('/api/rule', json=rule_update).json['data']['updated']
        assert len(updated) == 1

        audits = client.simulate_get('/api/audit').json['data']
        assert len(audits) == 2
        assert audits[1]['collection'] == 'rule'
        assert audits[1]['object_id'] == rule_uid
        assert audits[1]['action'] == 'updated'
        assert audits[1]['snapshot'].items() >= rule_update.items()

    def test_delete(self, client):
        rule = {'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]]}
        added = client.simulate_post('/api/rule', json=rule).json['data']['added']
        assert len(added) == 1
        rule_uid = added[0]['uid']

        deleted_count = client.simulate_delete(f'/api/rule/{rule_uid}').json['count']
        assert deleted_count == 1

        audits = client.simulate_get('/api/audit').json['data']
        for audit in audits:
            print(audit)
        assert len(audits) == 2
        assert audits[1]['collection'] == 'rule'
        assert audits[1]['object_id'] == rule_uid
        assert audits[1]['action'] == 'deleted'
        assert audits[1]['snapshot'] == {}

    def test_create_error(self, client):
        rule = {'name': 'rule01', 'condition': ['MATCHES', 'a', '['], 'modifications': [['SET', 'x', 1]]}
        rejected = client.simulate_post('/api/rule', json=rule).json['data']['rejected']
        assert len(rejected) == 1

        audits = client.simulate_get('/api/audit').json['data']
        assert len(audits) == 1
        assert audits[0]['collection'] == 'rule'
        assert audits[0]['action'] == 'rejected'
        assert audits[0]['snapshot'].items() >= rule.items()
        assert isinstance(audits[0]['error'], str)
        assert isinstance(audits[0]['traceback'], list)

    def test_update_error(self, client):
        rule = {'name': 'rule01', 'condition': ['=', 'a', 'x'], 'modifications': [['SET', 'x', 1]]}
        added = client.simulate_post('/api/rule', json=rule).json['data']['added']
        assert len(added) == 1
        rule_uid = added[0]['uid']

        rule_update = {'uid': rule_uid, 'name': 'rule01', 'condition': ['MATCHES', 'a', '['], 'modifications': [['SET', 'x', 1]]}
        rejected = client.simulate_put('/api/rule', json=rule_update).json['data']['rejected']
        assert len(rejected) == 1

        audits = client.simulate_get('/api/audit').json['data']
        assert len(audits) == 2
        assert audits[1]['collection'] == 'rule'
        assert audits[1]['object_id'] == rule_uid
        assert audits[1]['action'] == 'rejected'
        assert audits[1]['snapshot'].items() >= rule_update.items()
