#!/usr/bin/python
import os
import json
import falcon
from bson.json_util import loads, dumps
from bson.errors import BSONError
from json import JSONDecodeError
from logging import getLogger
log = getLogger('snooze.api')

from snooze.plugins.core.basic.falcon.route import Route
from snooze.api.falcon import authorize

class CommentRoute(Route):
    @authorize
    def on_post(self, req, resp):
        if self.update_records(req, resp):
            super(CommentRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        if self.update_records(req, resp):
            super(CommentRoute, self).on_put(req, resp)

    def update_records(self, req, resp):
        update_records = []
        for req_media in req.media:
            if 'record_uid' in req_media:
                record = self.search('record', req_media['record_uid'])
                log.debug("Search record {}".format(req_media['record_uid']))
                if record['count'] > 0:
                    log.debug("Found record {}".format(record))
                    media_type = req_media.get('type')
                    comment = self.search('comment', ['=', 'record_uid', req_media['record_uid']])
                    record['data'][0]['comment_count'] = comment['count'] + 1
                    if media_type == 'ack' or media_type == 'esc':
                        record['data'][0]['state'] = media_type
                    update_records.append(record['data'][0])
                else:
                    resp.content_type = falcon.MEDIA_TEXT
                    resp.status = falcon.HTTP_503
                    resp.text = 'Record uid {} was not found'.format(req_media['record_uid'])
                    return False
            else:
                resp.content_type = falcon.MEDIA_TEXT
                resp.status = falcon.HTTP_503
                resp.text = 'Comments must contain records uid'
                return False
        if update_records:
            log.debug("Update records {}".format(update_records))
            self.core.db.write('record', update_records)
        return True
