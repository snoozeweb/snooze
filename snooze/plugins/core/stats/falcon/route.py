#
# Copyright 2018-2020 Florian Dematraz <florian.dematraz@snoozeweb.net>
# Copyright 2018-2020 Guillaume Ludinard <guillaume.ludi@gmail.com>
# Copyright 2020-2021 Japannext Co., Ltd. <https://www.japannext.co.jp/>
# SPDX-License-Identifier: AFL-3.0
#

#!/usr/bin/python

import falcon
from datetime import datetime, timedelta
from bson.json_util import loads, dumps
from logging import getLogger
log = getLogger('snooze.stats')

from snooze.api.base import BasicRoute
from snooze.api.falcon import authorize
from bson.json_util import loads, dumps
from urllib.parse import unquote

class StatsRoute(BasicRoute):
    @authorize
    def on_get(self, req, resp):
        now = datetime.now()
        date_from = int(req.params.get('date_from', (now - timedelta(days=1)).timestamp()))
        date_from = datetime.fromtimestamp(date_from).astimezone()
        date_until = int(req.params.get('date_until', now.timestamp()))
        date_until = datetime.fromtimestamp(date_until).astimezone()
        groupby = req.params.get('groupby', 'hour')
        resp.content_type = falcon.MEDIA_JSON
        result_dict = self.core.db.compute_stats('stats', date_from, date_until, groupby)
        if result_dict:
            result = dumps(result_dict)
            resp.body = result
            resp.status = falcon.HTTP_200
        else:
            resp.body = '{}'
            resp.status = falcon.HTTP_404
            pass
