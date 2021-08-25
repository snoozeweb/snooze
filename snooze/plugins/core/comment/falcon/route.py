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
from snooze.utils import Condition, Modification

class CommentRoute(Route):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.notification_plugin = self.core.get_core_plugin('notification')

    @authorize
    def on_post(self, req, resp):
        if self.update_records(req, resp):
            super(CommentRoute, self).on_post(req, resp)

    @authorize
    def on_put(self, req, resp):
        for req_media in req.media:
            req_media['edited'] = True
        if self.update_records(req, resp):
            super(CommentRoute, self).on_put(req, resp)

    @authorize
    def on_delete(self, req, resp, search='[]'):
        if self.delete_records(req, resp, search='[]'):
            super(CommentRoute, self).on_delete(req, resp, search)

    def update_records(self, req, resp):
        update_records = []
        record_comments = {}
        for req_media in req.media:
            if 'record_uid' in req_media:
                record_uid = req_media['record_uid']
                record_comments.setdefault(record_uid, []).append(req_media)
                records = self.search('record', record_uid)
                log.debug("Search record {}".format(record_uid))
                if records['count'] > 0:
                    log.debug("Found record {}".format(records))
                    media_type = req_media.get('type')
                    comments = self.search('comment', ['=', 'record_uid', record_uid], nb_per_page=0, page_number=1, order_by='date', asc=False)
                    records['data'][0]['comment_count'] = comments['count'] + len(record_comments[record_uid])
                    if media_type in ['ack', 'esc', 'open', 'close']:
                        log.debug("Changing record {} type to {}".format(record_uid, media_type))
                        records['data'][0]['state'] = media_type
                        modification_raw = req_media.get('modifications', [])
                        if media_type in ['esc', 'open']:
                            try:
                                modified = False
                                for modification in modification_raw:
                                    if Modification(*modification).modify(records['data'][0]):
                                        modified = True
                                if modified and self.notification_plugin:
                                    self.notification_plugin.process(records['data'][0])
                            except:
                                pass
                    update_records.append(records['data'][0])
                else:
                    resp.content_type = falcon.MEDIA_TEXT
                    resp.status = falcon.HTTP_503
                    resp.text = 'Record uid {} was not found'.format(record_uid)
                    return False
            else:
                resp.content_type = falcon.MEDIA_TEXT
                resp.status = falcon.HTTP_503
                resp.text = 'Comments must contain records uid'
                return False
        if update_records:
            log.debug("Replace records {}".format(update_records))
            self.core.db.write('record', update_records, duplicate_policy='replace')
        return True

    def delete_records(self, req, resp, search):
        update_records = []
        record_comments = {}
        if 'uid' in req.params:
            cond_or_uid = ['=', 'uid', req.params['uid']]
        else:
            string = req.params.get('s') or search
            try:
                cond_or_uid = loads(string)
            except:
                cond_or_uid = string
        comments = self.search('comment', cond_or_uid)
        if comments['count'] > 0:
            for comment in comments['data']:
                record_uid = comment['record_uid']
                record_comments.setdefault(record_uid, []).append(comment['uid'])
                records = self.search('record', record_uid)
                log.debug("Search record {}".format(record_uid))
                if records['count'] > 0:
                    log.debug("Found record {}".format(records))
                    comments = self.search('comment', ['=', 'record_uid', record_uid], nb_per_page=0, page_number=1, order_by='date', asc=False)
                    records['data'][0]['comment_count'] = comments['count'] - len(record_comments[record_uid])
                    relevant_comments = [com for com in comments['data'] if ((com.get('uid') not in record_comments[record_uid]) and (com.get('type') in ['ack', 'esc', 'open', 'close']))]
                    log.debug("Relevant comments: {}".format(relevant_comments))
                    if len(relevant_comments) > 0:
                        new_type = relevant_comments[0]['type']
                        log.debug("Reverting record {} type to {}".format(record_uid, new_type))
                        records['data'][0]['state'] = new_type
                    else:
                        log.debug("Resetting record {} type".format(record_uid))
                        records['data'][0]['state'] = ''
                    update_records.append(records['data'][0])
                else:
                    resp.content_type = falcon.MEDIA_TEXT
                    resp.status = falcon.HTTP_503
                    resp.text = 'Record uid {} was not found'.format(record_uid)
                    return False
        else:
            resp.content_type = falcon.MEDIA_TEXT
            resp.status = falcon.HTTP_503
            resp.text = 'No record was found matching this comment uid'
            return False
        if update_records:
            log.debug("Update records {}".format(update_records))
            self.core.db.write('record', update_records)
        return True
