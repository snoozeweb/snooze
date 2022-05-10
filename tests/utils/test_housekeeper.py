'''Tests for the housekeeper'''

from datetime import datetime, timedelta

from snooze.utils.housekeeper import Housekeeper

class TestHousekeeper:

    def test_cleanup_alert(self, db, config):
        now = datetime.now()
        last_week = now - timedelta(days=7)
        yesterday = now - timedelta(days=1)
        ttl = timedelta(days=3).total_seconds()
        records = [
            {'name': '1', 'date_epoch': last_week.timestamp(), 'ttl': ttl},
            {'name': '2', 'date_epoch': yesterday.timestamp(), 'ttl': ttl},
        ]
        for record in records:
            db.write('record', record, update_time=False)
        housekeeper = Housekeeper(config.housekeeper, config.core.backup, db)
        job = housekeeper.jobs['cleanup_alert']
        job.run(db)
        results = db.search('record')['data']
        assert len(results) == 1
        assert results[0]['name'] == '2'

    def test_cleanup_comment(self, db, config):
        now = datetime.now()
        last_week = now - timedelta(days=7)
        yesterday = now - timedelta(days=1)
        ttl = timedelta(days=3).total_seconds()
        records = [
            {'name': '1', 'date_epoch': last_week.timestamp(), 'ttl': ttl},
            {'name': '2', 'date_epoch': yesterday.timestamp(), 'ttl': ttl},
        ]
        db.write('record', records, update_time=False)
        records = db.search('record')['data']
        comments = [
            {'record_uid': records[0]['uid'], 'message': 'comment 1'},
            {'record_uid': records[0]['uid'], 'message': 'comment 2'},
            {'record_uid': records[1]['uid'], 'message': 'comment 3'},
            {'record_uid': 'unknown', 'message': 'comment 4'}
        ]
        db.write('comment', comments)
        housekeeper = Housekeeper(config.housekeeper, config.core.backup, db)
        job = housekeeper.jobs['cleanup_comment']
        job.run(db)
        results = db.search('comment')['data']
        assert len(results) == 3
        assert results[0]['message'] == 'comment 1'
        assert results[1]['message'] == 'comment 2'
        assert results[2]['message'] == 'comment 3'
