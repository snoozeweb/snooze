import json
import yaml
import os
import re
import logging
import uuid
import time
import html
import threading
import falcon
from datetime import datetime, timedelta
from dateutil import parser
from pathlib import Path
from string import Template
from types import SimpleNamespace
from urllib.parse import urlparse, parse_qs, unquote
from snooze_client import Snooze
from snooze_teams.bot_parser import parser as bot_parser
from snooze_teams.bot_emoji import parse_emoji

from waitress.adjustments import Adjustments
from waitress.server import TcpWSGIServer
from O365 import Account, MSGraphProtocol

LOG = logging.getLogger("snooze.teamschat")

class SnoozeBot():

    def __init__(self, env_name, file_name, plugin_class = "SnoozeBotPlugin"):
        self.reload_config(env_name, file_name)
        level = logging.INFO
        if self.config.get('debug', False):
            level = logging.DEBUG
        logformat = "%(asctime)s - %(name)s: %(levelname)s - %(message)s"
        logging.getLogger("").handlers.clear()
        logging.basicConfig(format=logformat, level=level)
        self.plugin = globals()[plugin_class](self.config)

    def reload_config(self, env_name, file_name):
        config_path = Path(os.environ.get(env_name, '/etc/snooze'))
        config_file = config_path / file_name
        if config_file.exists():
            self.config = yaml.safe_load(config_file.read_text())
        else:
            self.config = {}

class SnoozeBotPlugin():

    date_regex = re.compile(r"[0-9]{1,4}-[0-9]{1,2}-[0-9]{1,2}T[0-9]{1,2}:[0-9]{1,2}:[0-9]{1,2}[\+\d]*")
    duration_regex = re.compile(r"((\d+) *(mins|min|m|hours|hour|h|weeks|week|w|days|day|d|months|month|years|year|y)|forever){0,1} *(.*)", re.IGNORECASE)

    def __init__(self, config):
        self.config = config
        self.address = self.config.get('listening_address', '0.0.0.0')
        self.port = self.config.get('listening_port', 5202)
        self.date_format = self.config.get('date_format', '%a, %b %d, %Y at %I:%M %p')
        self.client = Snooze()
        self.bot_name = self.config.get('bot_name', 'Bot')
        self.snooze_url = self.config.get('snooze_url', 'http://localhost:5201')
        if self.snooze_url.endswith('/'):
            self.snooze_url = self.snooze_url[:-1]
        self.message_limit = self.config.get('message_limit', 10)
        self.snooze_limit = self.config.get('snooze_limit', self.message_limit)
    
    def process_alert(self, request, medias):
        if not isinstance(medias, list):
            medias = [medias]
        response = self.process_records(request, medias)
        LOG.debug("Response: {}".format(response))
        return response

    def send_message(self, message, channel_id="", thread={}, attachment={}, request={}, layout_type='post'):
        return

    def process_records(self, req, medias):
        multi = len(medias) > 1
        channels = {}
        header = False
        footer = False
        return_value = {}
        website = self.snooze_url
        action_name = req.params['snooze_action_name']
        for req_media in medias[:self.message_limit]:
            self.process_rec(channels, req_media, action_name, multi, website)
        for req_media in medias[self.message_limit:]:
            self.process_rec(channels, req_media, action_name, multi, website, False)
        for channel, content in channels.items():
            if multi:
                header = True
                if len(content) > self.message_limit:
                    footer = True
            # Parse channel_id: if it contains /messages/{id}, extract parent thread for replies
            actual_channel = channel
            channel_parent_thread = {}
            match = re.match(r'^(.+)/messages/(.+)$', channel)
            if match:
                actual_channel = match.group(1)
                channel_parent_thread = {'channel_id': actual_channel, 'thread_id': match.group(2)}
            if hasattr(self, 'register_poll_resource'):
                self.register_poll_resource(actual_channel)
            # Detect channel layout type (post or chat)
            layout_type = self.get_channel_layout(actual_channel) if hasattr(self, 'get_channel_layout') else 'post'
            attachment = [{'text': 'Acknowledge', 'action': 'ack', 'style': 'success'}, {'text': 'Close', 'action': 'close', 'style': 'primary'}]
            if not multi and content[0]['threads']:
                for thread in content[0]['threads']:
                    self.send_message(content[0]['msg'], channel_id=actual_channel, thread=thread, attachment=attachment, request=req, layout_type=layout_type)
                return_value = {content[0]['record_hash']: {'threads': content[0]['threads'], 'multithreads': content[0]['multithreads']}}
            else:
                resp = self.send_message({'header': header, 'footer': footer, 'messages': content}, channel_id=actual_channel, thread=channel_parent_thread, attachment=attachment, request=req, layout_type=layout_type)
                if resp:
                    for message in content:
                        if channel_parent_thread:
                            t = channel_parent_thread.copy()
                        else:
                            t = {'channel_id': actual_channel, 'thread_id': resp.get('root_id', resp['id'])}
                        if multi:
                            message['multithreads'].append(t)
                        else:
                            message['threads'].append(t)
                        return_value[message['record_hash']] = {'threads': message['threads'], 'multithreads': message['multithreads']}
        if multi:
            return return_value
        else:
            return list(return_value.values())[0]

    def process_rec(self, channels,req_media, action_name, multi, website, process = True):
        rec_channels = req_media['channels']
        record = req_media['alert']
        message = req_media.get('message')
        message_group = req_media.get('message_group')
        reply = req_media.get('reply')
        notification_from = record.get('notification_from')

        LOG.debug('Received record: {}'.format(record))
        msg = {}
        threads = next((action_result.get('content', {}).get('threads', []) for action_result in record.get('snooze_webhook_responses', []) if action_result.get('action_name') == action_name), [])
        multithreads = next((action_result.get('content', {}).get('multithreads', []) for action_result in record.get('snooze_webhook_responses', []) if action_result.get('action_name') == action_name), [])
        if process:
            msg['record'] = record
            if multi:
                msg['multi'] = True
            if threads:
                msg['threads'] = True
            if reply:
                msg['reply'] = reply
            if message:
                msg['message'] = message
            if message_group:
                msg['message_group'] = message_group
            if notification_from:
                notif_name = notification_from.get('name', 'anonymous')
                notif_message = notification_from.get('message')
                msg['from'] = notif_name
                if notif_message:
                    msg['notif_msg'] = notif_message
        for channel in rec_channels:
            if channel not in channels:
                channels[channel] = []
            channels[channel].append({'msg': msg, 'record_hash': record.get('hash', ''), 'threads': threads, 'multithreads': multithreads})

    def process_user_message(self, message):
        LOG.debug("Received message: '{}'".format(vars(message)))
        try:
            display_name = message.user_name
            thread = message.root_id
            if 'command' in message.body:
                original_message = message.command + ' ' + message.text.lstrip()
            else:
                original_message = message.text.lstrip()
        except:
            display_name = message.sender_name
            original_message = message.text.lstrip()
            thread = message.root_id or message.id
        try:
            command, text = re.split(r'[^a-zA-Z0-9\/]', original_message, 1)
        except ValueError:
            command = original_message
            text = ''
        command = command.casefold()
        link = ''
        snoozelink = ''
        modification = []
        snooze_help = """`{}`: Command: **@{}** snooze <duration> [condition]

**duration** (forever or X mins|min|m|hours|hour|h|weeks|week|w|days|day|d|months|month|years|year|y): *Duration of this snooze entry*
**condition** (text): *Condition for which this snooze entry will match*

Example: *@{}* **snooze** 6h host = example_host""".format(display_name, self.bot_name, self.bot_name)
        if command in ['help_snooze', '/help_snooze']:
            return snooze_help
        elif not command or command in ['help', '/help']:
            if text == 'snooze':
                return snooze_help
            else:
                return """`{}`: List of available commands:

**ack, acknowledge, ok** [message]: *Acknowledge an alert*
**esc, escalate, re-escalate, reescalate, re-esc, reesc** <modification> [message]: *Re-escalate an alert*
**close, done** [message]: *Close an alert*
**open, reopen, re-open** [message]: *Re-open an alert*
**snooze** <duration> [condition]: *Snooze an alert (default 1h) (*`/help_snooze`*)*
any other message: *Comment an alert*

Example: *@{}* **esc** severity = critical *Please check*""".format(display_name, self.bot_name)
        aggregates = self.client.record([
            'OR',
            ['IN', ['IN', thread, 'content.threads.root_id'], 'snooze_webhook_responses'],
            ['IN', ['IN', thread, 'content.threads.thread_id'], 'snooze_webhook_responses'],
            ['IN', ['IN', thread, 'content.multithreads.root_id'], 'snooze_webhook_responses'],
            ['IN', ['IN', thread, 'content.multithreads.thread_id'], 'snooze_webhook_responses']
        ])
        if len(aggregates) == 0:
            return ':x: `{}`:Cannot find the corresponding alert! (command: `{}`)'.format(display_name, original_message)
        record = aggregates[0]
        action_name = next(action_result.get('action_name') for action_result in record.get('snooze_webhook_responses', [])
            if thread in list(map(
                lambda x: x.get('root_id') or x.get('thread_id'),
                action_result.get('content', {}).get('threads', []) + action_result.get('content', {}).get('multithreads', [])
            ))) or 'SnoozeBot'
        user = '{} via {}'.format(display_name, action_name)
        if self.snooze_url:
            link = '[[Link]]({}/web/?#/record?tab=All&s=hash%3D{})'.format(self.snooze_url, record['hash'])
            snoozelink = '[[Link]]({}/web/?#/snooze?tab=All&s=hash%3D{})'.format(self.snooze_url, record['hash'])
        if command in ['snooze', '/snooze']:
            LOG.debug("Snooze {} alerts with parameters: '{}'".format(len(aggregates), text))
            duration_match = SnoozeBotPlugin.duration_regex.search(text)
            if duration_match:
                try:
                    now = datetime.now()
                    time_constraints = {}
                    conditions = []
                    query = ''
                    duration_time = duration_match.group(1)
                    duration_number = duration_match.group(2)
                    duration_period = duration_match.group(3)
                    query_match = duration_match.group(4)
                    if duration_time and duration_time == 'forever':
                        later = None
                        duration = 'Forever'
                    elif duration_period:
                        duration_period = duration_period.casefold()
                        if duration_period.startswith('h'):
                            later = now + timedelta(hours = int(duration_number))
                            duration = duration_number + ' hour(s)'
                        elif duration_period.startswith('d'):
                            later = now + timedelta(days = int(duration_number))
                            duration = duration_number + ' day(s)'
                        elif duration_period.startswith('w'):
                            later = now + timedelta(weeks = int(duration_number))
                            duration = duration_number + ' week(s)'
                        elif duration_period.startswith('month'):
                            later = now + timedelta(days = int(duration_number)*30)
                            duration = duration_number + ' month(s)'
                        elif duration_period.startswith('m'):
                            later = now + timedelta(minutes = int(duration_number))
                            duration = duration_number + ' minute(s)'
                        elif duration_period.startswith('y'):
                            later = now + timedelta(days = int(duration_number)*365)
                            duration = duration_number + ' year(s)'
                        else:
                            return ":x: `{}`: Invalid snooze filter duration syntax. Use `/help_snooze` to learn how to use this command".format(display_name)
                    else:
                        later = now + timedelta(hours = 1)
                        duration = '1h'
                    if query_match:
                        query = query_match
                        conditions = [None]
                    else:
                        conditions = []
                        for record in aggregates:
                            conditions.append(['=', 'hash', '{}'.format(record['hash'])])
                    if later:
                        time_constraints = {"datetime": [{"from": now.astimezone().strftime("%Y-%m-%dT%H:%M:%S%z"), "until": later.astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")}]}
                    if len(conditions) <= self.snooze_limit:
                        payload = [{'name': '[{}] {} ({})'.format(duration, display_name, str(uuid.uuid4())[:5]), 'condition': condition, 'ql': query, 'time_constraints': time_constraints, 'comment': display_name} for condition in conditions]
                        result = self.client.snooze_batch(payload)
                        if result.get('rejected'):
                            return ':x: `{}`: Could not Snooze alert(s)!'.format(display_name)
                        LOG.debug('Done: {}'.format(result))
                        ack_payload = [{'type': 'ack', 'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': 'Snoozed for {}'.format(duration)} for record in aggregates]
                        self.client.comment_batch(ack_payload)
                        count = ''
                        if len(aggregates) > 1:
                            link = '[[Link]]({}/web/?#/record?tab=Acknowledged)'.format(self.snooze_url)
                            snoozelink = '[[Link]]({}/web/?#/snooze?tab=All)'.format(self.snooze_url)
                            count = '**{}** '.format(len(aggregates))
                        comment_text = ":white_check_mark: {}Alert(s) acknowledged successfully by `{}`! {}\n".format(count, display_name, link)
                        warning_text = ''
                        if len(result['data'].get('added', [])) > 0:
                            res_cond = result['data']['added'][0].get('condition', [])
                            if len(res_cond) > 0 and res_cond[0] == 'SEARCH':
                                warning_text = "\n:warning: Snooze filter condition `{}` might not be expected. Please double check in the Web interface".format(res_cond)
                        if later:
                            return comment_text + ':white_check_mark: Snoozed for {}! Expires at **{}** {}'.format(duration, later.strftime(self.date_format), snoozelink) + warning_text
                        else:
                            return comment_text + ':white_check_mark: Snoozed forever! {}'.format(snoozelink) + warning_text
                    else:
                        return ':x: `{}`: Cannot Snooze more than {} alert(s) without using an explicit condition. Please try again or use [SnoozeWeb]({}/web/?#/snooze).'.format(display_name, self.snooze_limit, self.snooze_url)
                except Exception as e:
                    LOG.exception(e)
                    return ':x: `{}`: Could not Snooze alert(s)!'.format(display_name)
            else:
                return "`{}`: Invalid Snooze filter syntax. Use `/help_snooze` to learn how to use this command".format(display_name)
        elif command in ['ack', 'acknowledge', 'ok', '/ack']:
            LOG.debug('ACK {} alerts'.format(len(aggregates)))
            try:
                payload = [{'type': 'ack', 'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': text} for record in aggregates]
                self.client.comment_batch(payload)
                msg_extra = ''
                if text:
                    msg_extra = ' with message `{}`'.format(text)
                if len(aggregates) == 1:
                    return ':white_check_mark: Alert acknowledged successfully by `{}`{}! {}'.format(display_name, msg_extra, link)
                else:
                    return ':white_check_mark: **{}** alerts acknowledged successfully by `{}`{}! [[Link]]({}/web/?#/record?tab=Acknowledged)'.format(len(aggregates), display_name, msg_extra, self.snooze_url)
            except Exception as e:
                LOG.exception(e)
                return ':x: `{}`: Could not acknowledge alert(s)!'.format(display_name)
        elif command in ['esc', 'escalate', 're-escalate', 'reescalate', 're-esc', 'reesc', '/esc']:
            LOG.debug('ESC {} alerts'.format(len(aggregates)))
            try:
                modifications, comment = bot_parser(text)
                payload = [{'type': 'esc', 'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': comment, 'modifications': modifications} for record in aggregates]
                self.client.comment_batch(payload)
                msg_extra = ''
                if modifications:
                    msg_extra += ' with modification `{}`'.format(modifications)
                if comment:
                    msg_extra += ' {} message `{}`'.format('and' if modifications else 'with', comment)
                if len(aggregates) == 1:
                    return ':white_check_mark: Alert re-escalated successfully by `{}`{}! {}'.format(display_name, msg_extra, link)
                else:
                    return ':white_check_mark: **{}** alerts re-escalated successfully by `{}`{}! [[Link]]({}/web/?#/record?tab=Re-escalated)'.format(len(aggregates), display_name, msg_extra, self.snooze_url)
            except Exception as e:
                LOG.exception(e)
                return ':x: `{}`: Could not re-escalate alert(s)!'.format(display_name)
        elif command in ['close', 'done', '/close']:
            LOG.debug('CLOSE {} alerts'.format(len(aggregates)))
            try:
                payload = [{'type': 'close', 'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': text} for record in aggregates]
                self.client.comment_batch(payload)
                msg_extra = ''
                if text:
                    msg_extra = ' with message `{}`'.format(text)
                if len(aggregates) == 1:
                    return ':white_check_mark: Alert closed successfully by `{}`{}! {}'.format(display_name, msg_extra, link)
                else:
                    return ':white_check_mark: **{}** alerts closed successfully by `{}`{}! [[Link]]({}/web/?#/record?tab=Closed)'.format(len(aggregates), display_name, msg_extra, self.snooze_url)
            except Exception as e:
                LOG.exception(e)
                return ':x: `{}`: Could not close alert(s)!'.format(display_name)
        elif command in ['open', 'reopen', 're-open', '/open']:
            LOG.debug('OPEN {} alerts'.format(len(aggregates)))
            try:
                payload = [{'type': 'open', 'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': text} for record in aggregates]
                self.client.comment_batch(payload)
                msg_extra = ''
                if text:
                    msg_extra = ' with message `{}`'.format(text)
                if len(aggregates) == 1:
                    return ':white_check_mark: Alert re-opened successfully by `{}`{}! {}'.format(display_name, msg_extra, link)
                else:
                    return ':white_check_mark: **{}** alerts re-opened successfully by `{}`{}! [SnoozeWeb]({}/web/?#/record?tab=Alerts&s=state=open)'.format(len(aggregates), display_name, msg_extra, self.snooze_url)
            except Exception as e:
                LOG.exception(e)
                return ':x: `{}`: Could not re-open alert(s)!'.format(display_name)
        else:
            LOG.debug('COMMENT {} alerts'.format(len(aggregates)))
            try:
                msg_extra = ''
                if command in ['/comment']:
                    msg_extra = text
                else:
                    msg_extra = original_message
                payload = [{'record_uid': record['uid'], 'name': user, 'method': 'teams', 'message': msg_extra} for record in aggregates]
                self.client.comment_batch(payload)
                if len(aggregates) == 1:
                    return ':white_check_mark: Comment added successfully by `{}`: `{}`! {}'.format(display_name, msg_extra, link)
                else:
                    return ':white_check_mark: **{}** comments added successfully by `{}`: `{}`! [[Link]]({}/web/?#/record)'.format(len(aggregates), display_name, msg_extra, self.snooze_url)
            except Exception as e:
                LOG.exception(e)
                return ':x: `{}`: Could not comment alert(s)!'.format(display_name)

class AlertRoute():

    def __init__(self, plugin):
        self.plugin = plugin

    def on_post(self, req, resp):
        medias = req.media
        if not isinstance(medias, list):
            medias = [medias]
        response = self.plugin.process_records(req, medias)
        LOG.debug("Response: {}".format(response))
        resp.status = falcon.HTTP_200
        if response:
            resp.content_type = falcon.MEDIA_JSON
            resp.media = response

class TeamsPlugin(SnoozeBotPlugin):

    BOT_MARKER = '<!-- snooze-bot -->'

    def __init__(self, config):
        super().__init__(config)
        self._channel_layout_cache = {}
        self.poll_interval_seconds = int(self.config.get('poll_interval_seconds', 10))
        self.poll_lookback_seconds = int(self.config.get('poll_lookback_seconds', 0))
        resources = self.config.get('poll_resources', [])
        if isinstance(resources, str):
            resources = [resources]
        self._poll_resources = set()
        self._poll_resources_lock = threading.Lock()
        self._poller = None
        self.self_user_id = ''
        self.self_user_name = ''
        for resource in resources:
            normalized = self.normalize_poll_resource(resource)
            if normalized:
                self._poll_resources.add(normalized)

    def normalize_poll_resource(self, resource):
        if not resource:
            return ''
        try:
            return self.build_messages_url(resource)
        except Exception:
            return resource

    def _normalize_channel_ref(self, channel_ref):
        if not channel_ref:
            return channel_ref
        if '@thread.tacv2' in channel_ref:
            return channel_ref
        return '{}@thread.tacv2'.format(channel_ref)

    def _normalize_channels_in_path(self, path):
        # Normalize /channel/ (singular) to /channels/ (plural)
        path = re.sub(r'/channel/', '/channels/', path)
        def add_suffix(match):
            return '/channels/{}'.format(self._normalize_channel_ref(match.group(1)))
        return re.sub(r'/channels/([^/\?]+)', add_suffix, path)

    def on_alert(self, request, medias):
        response = self.process_alert(request, medias)

    def register_poll_resource(self, channel_id):
        if not channel_id:
            return
        normalized = self.normalize_poll_resource(channel_id)
        with self._poll_resources_lock:
            self._poll_resources.add(normalized)

    def get_poll_resources(self):
        with self._poll_resources_lock:
            return list(self._poll_resources)

    def build_messages_url(self, resource):
        if resource.startswith('https://teams.microsoft.com/l/channel/'):
            parsed = urlparse(resource)
            path_parts = parsed.path.split('/')
            channel_id = unquote(path_parts[3]) if len(path_parts) > 3 else ''
            group_id = parse_qs(parsed.query).get('groupId', [''])[0]
            if channel_id and group_id:
                return 'https://graph.microsoft.com/beta/teams/{}/channels/{}/messages'.format(group_id, self._normalize_channel_ref(channel_id))
        if resource.startswith('https://graph.microsoft.com/'):
            return self._normalize_channels_in_path(resource)
        if resource.startswith('/'):
            return 'https://graph.microsoft.com/beta{}'.format(self._normalize_channels_in_path(resource))
        if resource.endswith('/messages'):
            return 'https://graph.microsoft.com/beta/{}'.format(self._normalize_channels_in_path(resource))
        if resource.endswith('@thread.tacv2'):
            return 'https://graph.microsoft.com/beta/{}/messages'.format(resource)
        if resource.startswith('teams/'):
            return 'https://graph.microsoft.com/beta/{}/messages'.format(self._normalize_channels_in_path(resource))
        return 'https://graph.microsoft.com/beta/{}@thread.tacv2/messages'.format(resource)

    def build_post_messages_url(self, channel_id, thread_id=''):
        if channel_id.startswith('https://teams.microsoft.com/l/channel/'):
            base_url = self.build_messages_url(channel_id)
        elif channel_id.startswith('https://graph.microsoft.com/'):
            base_url = self._normalize_channels_in_path(channel_id)
            if not base_url.endswith('/messages'):
                base_url = '{}/messages'.format(base_url.rstrip('/'))
        elif channel_id.startswith('teams/') or channel_id.startswith('/teams/'):
            rel = self._normalize_channels_in_path(channel_id.lstrip('/'))
            if rel.endswith('/messages'):
                base_url = 'https://graph.microsoft.com/beta/{}'.format(rel)
            else:
                base_url = 'https://graph.microsoft.com/beta/{}/messages'.format(rel)
        elif channel_id.endswith('@thread.tacv2'):
            base_url = 'https://graph.microsoft.com/beta/{}/messages'.format(channel_id)
        else:
            base_url = 'https://graph.microsoft.com/beta/{}@thread.tacv2/messages'.format(channel_id)
        if thread_id:
            return '{}/{}/replies'.format(base_url.rstrip('/'), thread_id)
        return base_url

    def build_channel_info_url(self, channel_id):
        if channel_id.startswith('https://teams.microsoft.com/l/channel/'):
            parsed = urlparse(channel_id)
            path_parts = parsed.path.split('/')
            channel_ref = unquote(path_parts[3]) if len(path_parts) > 3 else ''
            group_id = parse_qs(parsed.query).get('groupId', [''])[0]
            if channel_ref and group_id:
                return 'https://graph.microsoft.com/beta/teams/{}/channels/{}'.format(group_id, channel_ref)
        if channel_id.startswith('https://graph.microsoft.com/'):
            normalized = self._normalize_channels_in_path(channel_id)
            if normalized.endswith('/messages'):
                return normalized[:-9]
            return normalized
        if channel_id.startswith('teams/') or channel_id.startswith('/teams/'):
            rel = self._normalize_channels_in_path(channel_id.lstrip('/'))
            if rel.endswith('/messages'):
                rel = rel[:-9]
            return 'https://graph.microsoft.com/beta/{}'.format(rel)
        if channel_id.endswith('@thread.tacv2'):
            return 'https://graph.microsoft.com/beta/{}'.format(channel_id)
        return 'https://graph.microsoft.com/beta/{}@thread.tacv2'.format(channel_id)

    def fetch_messages(self, resource):
        url = self.build_messages_url(resource)
        resp = self.driver.con.get(url)
        data = resp.json()
        if isinstance(data, dict):
            return data.get('value', [])
        return []

    def fetch_replies(self, resource, message_id):
        url = '{}/{}/replies'.format(self.build_messages_url(resource).rstrip('/'), message_id)
        resp = self.driver.con.get(url)
        data = resp.json()
        if isinstance(data, dict):
            return data.get('value', [])
        return []

    def _strip_html(self, text):
        text = re.sub(r'<[^>]*>', ' ', text or '')
        text = html.unescape(text)
        return re.sub(r'\s+', ' ', text).strip()

    def _reply_to_html(self, text):
        raw = text or ''
        emoji_map = {
            ':white_check_mark:': '✅',
            ':x:': '❌',
            ':warning:': '⚠️',
        }
        for shortcode, symbol in emoji_map.items():
            raw = raw.replace(shortcode, symbol)
        rendered = html.escape(raw)
        rendered = re.sub(r'\[\[(.*?)\]\]\((.*?)\)', r'<a href="\2">\1</a>', rendered)
        rendered = re.sub(r'\[(.*?)\]\((.*?)\)', r'<a href="\2">\1</a>', rendered)
        rendered = re.sub(r'\*\*(.*?)\*\*', r'<b>\1</b>', rendered)
        rendered = re.sub(r'`([^`]+)`', r'<code>\1</code>', rendered)
        return rendered.replace('\n', '<br>')

    def is_self_message(self, graph_message):
        body_content = graph_message.get('body', {}).get('content', '') or ''
        if TeamsPlugin.BOT_MARKER in body_content:
            return True
        sender = graph_message.get('from', {})
        if sender.get('application'):
            return True
        user = sender.get('user', {})
        sender_id = user.get('id', '')
        sender_name = user.get('displayName', '')
        if self.self_user_id and sender_id == self.self_user_id:
            return True
        if self.self_user_name and sender_name.casefold() == self.self_user_name.casefold():
            return True
        return sender_name.casefold() == self.bot_name.casefold()

    def normalize_incoming_message(self, graph_message):
        sender = graph_message.get('from', {}).get('user', {})
        body = graph_message.get('body', {})
        content = body.get('content', '')
        if body.get('contentType', '').casefold() == 'html':
            text = self._strip_html(content)
        else:
            text = (content or '').strip()
        root_id = graph_message.get('replyToId') or graph_message.get('id')
        return SimpleNamespace(
            user_name=sender.get('displayName', 'unknown'),
            sender_name=sender.get('displayName', 'unknown'),
            text=text,
            id=graph_message.get('id'),
            root_id=root_id,
            body=body,
        )

    def reply_to_polled_message(self, response_text, channel_id, graph_message):
        if not response_text:
            return
        thread_id = graph_message.get('replyToId') or graph_message.get('id')
        if not thread_id:
            return
        layout_type = self.get_channel_layout(channel_id)
        self.send_message({'reply': response_text}, channel_id=channel_id, thread={'thread_id': thread_id}, layout_type=layout_type)

    def start_polling(self):
        if self._poller:
            return
        self._poller = TeamsPoller(self)
        self._poller.start()

    def stop_polling(self):
        if self._poller:
            self._poller.kill()
            self._poller = None

    def get_channel_layout(self, channel_id):
        """Auto-detect the channel layout type via the Graph API.

        Queries GET /beta/{channel_id}@thread.tacv2 and reads the layoutType property.
        Results are cached per channel_id.

        Returns:
            'post' (default) or 'chat'
        """
        if channel_id in self._channel_layout_cache:
            return self._channel_layout_cache[channel_id]
        try:
            url = self.build_channel_info_url(channel_id)
            resp = self.driver.con.get(url)
            data = resp.json()
            layout = data.get('layoutType', 'post')
            LOG.info("Channel %s has layout type: %s", channel_id, layout)
            self._channel_layout_cache[channel_id] = layout
            return layout
        except Exception as e:
            LOG.warning("Failed to detect channel layout for %s, defaulting to 'post': %s", channel_id, e)
            self._channel_layout_cache[channel_id] = 'post'
            return 'post'

    def send_message(self, message, channel_id="", thread={}, attachment={}, request={}, layout_type='post'):
        if layout_type == 'chat':
            data = self.format_flat_message(message, thread)
        else:
            data = self.format_message(message, thread)
        LOG.debug('Posting on {}'.format(channel_id))
        props = {}
        thread_id = ''
        if thread:
            thread_id = thread['thread_id']
        url = self.build_post_messages_url(channel_id, thread_id)
        for n in range(3):
            try:
                resp = self.driver.con.post(url, data=data)
                return resp.json()
            except Exception as e:
                LOG.exception(e)
                time.sleep(1)
                continue
        return None

    def serve(self):
        self.app = falcon.App()
        self.app.add_route('/alert', AlertRoute(self))
        wsgi_options = Adjustments(host=self.address, port=self.port)
        httpd = TcpWSGIServer(self.app, adj=wsgi_options)
        LOG.info("Serving on port {}...".format(str(self.port)))
        self.start_polling()
        try:
            httpd.run()
        finally:
            self.stop_polling()

    def format_message(self, message, thread):
        uid = uuid.uuid4().hex
        website = self.snooze_url
        one_message = message
        if len(message.get('messages', [])) == 1:
            one_message = message['messages'][0]['msg']
        from_message = ''
        if one_message.get('from'):
            from_message = 'From **{}**'.format(one_message.get('from'))
            if one_message.get('from_msg'):
                from_message += ': {}'.format(one_message.get('from_msg'))
        if not 'messages' in message:
            simple_message = TeamsPlugin.BOT_MARKER
            if from_message:
                simple_message += from_message + '<br>'
            if message.get('reply'):
                reply_text = SnoozeBotPlugin.date_regex.sub(lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format), message.get('reply'))
                simple_message += self._reply_to_html(reply_text)
            else:
                record = message['record']
                timestamp = SnoozeBotPlugin.date_regex.sub(lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format), record.get('timestamp', str(datetime.now().astimezone())))
                msg = parse_emoji("::warning:: <b>New escalation</b> on {} ::warning::".format(timestamp))
                if len(record.get('message', '')) > 0:
                    msg += '<br>{}'.format(record.get('message'))
                simple_message += msg
            return {'body': {'content': simple_message, "contentType": "html"}}
        if message.get('header'):
            header = parse_emoji('::warning:: Received {} alerts ::warning::'.format(len(message['messages'])))
        else:
            header = parse_emoji('::warning:: Received alert ::warning::')
        footer_msg = ''
        if message.get('footer'):
            footer_msg = 'Check all alerts in [Snoozeweb]({}/web)'.format(website)
        elif len(message['messages']) == 1:
            footer_msg = message['messages'][0]['msg']['record'].get('message', 'No message')
        footer = Template(""",{
                        "type": "TextBlock",
                        "text": "$footer_msg",
                        "wrap": true
                    }""").substitute({'footer_msg': footer_msg})
        timestamp = SnoozeBotPlugin.date_regex.sub(lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format), message['messages'][0]['msg']['record'].get('timestamp', datetime.now().astimezone()))
        if len(message['messages']) == 1:
            record = message['messages'][0]['msg']['record']
            facts_list = [('Host', '[{}]({}/web/?#/record?tab=All&s=hash%3D{})'.format(record.get('host', 'Unknown'), website, record.get('hash'))), ('Source', record.get('source', 'Unknown')), ('Process', record.get('process', 'Unknown')), ('Severity', record.get('severity', 'Unknown'))]
            facts = ','.join([Template('{"title": "$key", "value": "$value"}').substitute({'key': key, 'value': value}) for key, value in facts_list])
        else:
            messages = []
            for message in message['messages']:
                if message['msg'].get('record'):
                    msg = {}
                    msg['key'] = '[{}]({}/web/?#/record?tab=All&s=hash%3D{})'.format(message['msg']['record'].get('host', 'Unknown'), website, message['msg']['record'].get('hash'))
                    if message['msg'].get('threads'):
                        msg['key'] += ' (e)'
                    msg['value'] = '[{source}] **{process}** {message}'.format(source=message['msg']['record'].get('source', 'Unknown'), process=message['msg']['record'].get('process', 'Unknown'), message=message['msg']['record'].get('message', ''))
                    if message['msg'].get('from'):
                        from_msg = 'From **{}**'.format(message['msg'].get('from'))
                        if message['msg'].get('from_msg'):
                            from_msg += ': {}'.format(message['msg'].get('from_msg'))
                        msg['value'] += ' ({})'.format(from_msg)
                    messages.append(Template('{"title": "$key", "value": "$value"}').substitute(msg))
#            facts = ','.join(json.dumps(messages))
            facts = ','.join(messages)
        card = {
            'body': {
                'contentType': 'html',
                'content': '<attachment id="{}"></attachment>{}'.format(uid, TeamsPlugin.BOT_MARKER)
            },
            'attachments': [{
                'id': uid,
                'contentType': 'application/vnd.microsoft.card.adaptive',
                'contentUrl': None,
                'content': Template('''{
                    "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
                    "type": "AdaptiveCard",
                    "version": "1.4",
                    "msteams": {
                        "width": "full"
                    },
                    "body": [{
                        "type": "ColumnSet",
                        "columns": [{
                            "type": "Column",
                            "items": [{
                                "type": "TextBlock",
                                "weight": "Bolder",
                                "text": "$header",
                                "wrap": true
                            },{
                                "type": "TextBlock",
                                "spacing": "None",
                                "text": "$timestamp",
                                "isSubtle": true,
                                "wrap": true
                            },{
                                "type": "TextBlock",
                                "spacing": "None",
                                "text": "$from",
                                "wrap": true
                            }],
                            "width": "stretch"
                        }]},{
                            "type": "FactSet",
                            "facts": [$facts]
                        }$footer]
                }''').substitute({'schema': '$schema', 'header': header, 'footer': footer, 'timestamp': timestamp, 'facts': facts, 'from': from_message}),
                'name': "Testing name",
                'thumbnailUrl': None,
                'teamsAppId': '5ef9989f-aeae-45d5-a672-10615a4819c9'
            }]
        }
        LOG.info(card)
        return card

    def format_flat_message(self, message, thread):
        """Format a flat HTML message for chat-layout (Threads) channels.

        Unlike format_message which produces adaptive cards, this creates
        simple HTML content suitable for chat-style channels.
        """
        website = self.snooze_url
        one_message = message
        if len(message.get('messages', [])) == 1:
            one_message = message['messages'][0]['msg']
        from_message = ''
        if one_message.get('from'):
            from_message = 'From <b>{}</b>'.format(one_message.get('from'))
            if one_message.get('from_msg'):
                from_message += ': {}'.format(one_message.get('from_msg'))

        if not 'messages' in message:
            # Single re-escalation message (reply to existing thread)
            simple_message = TeamsPlugin.BOT_MARKER
            if from_message:
                simple_message += from_message + '<br>'
            if message.get('reply'):
                reply_text = SnoozeBotPlugin.date_regex.sub(lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format), message.get('reply'))
                simple_message += self._reply_to_html(reply_text)
            else:
                record = message['record']
                timestamp = SnoozeBotPlugin.date_regex.sub(lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format), record.get('timestamp', str(datetime.now().astimezone())))
                msg = parse_emoji("::warning:: <b>New escalation</b> on {} ::warning::".format(timestamp))
                if len(record.get('message', '')) > 0:
                    msg += '<br>{}'.format(record.get('message'))
                simple_message += msg
            return {'body': {'content': simple_message, 'contentType': 'html'}}

        # Build flat HTML for one or multiple alerts
        parts = []
        if message.get('header'):
            parts.append(parse_emoji('::warning:: <b>Received {} alerts</b> ::warning::'.format(len(message['messages']))))
        else:
            parts.append(parse_emoji('::warning:: <b>Received alert</b> ::warning::'))

        if from_message:
            parts.append(from_message)

        if len(message['messages']) == 1:
            msg = message['messages'][0].get('msg', {})
            record = msg.get('record', {})
            if record:
                host = html.escape(record.get('host', 'Unknown'))
                source = html.escape(record.get('source', 'Unknown'))
                process = html.escape(record.get('process', 'Unknown'))
                severity = html.escape(record.get('severity', 'Unknown'))
                alert_message = html.escape(record.get('message', ''))
                record_hash = record.get('hash', '')
                timestamp = SnoozeBotPlugin.date_regex.sub(
                    lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format),
                    record.get('timestamp', str(datetime.now().astimezone()))
                )
                timestamp = html.escape(timestamp)

                link = '<a href="{}/web/?#/record?tab=All&s=hash%3D{}">{}</a>'.format(website, record_hash, host)
                parts.append('<i>{}</i>'.format(timestamp))
                parts.append('<hr>')
                parts.append('<b>Host</b>: {}'.format(link))
                parts.append('<b>Source</b>: {}'.format(source))
                parts.append('<b>Process</b>: {}'.format(process))
                parts.append('<b>Severity</b>: {}'.format(severity))
                if alert_message:
                    parts.append('{}'.format(alert_message))
                if msg.get('from'):
                    notif = 'From <b>{}</b>'.format(html.escape(msg.get('from')))
                    if msg.get('from_msg'):
                        notif += ': {}'.format(html.escape(msg.get('from_msg')))
                    parts.append(notif)
                if msg.get('message'):
                    parts.append('<br><i>{}</i>'.format(html.escape(msg.get('message'))))

                if message.get('footer'):
                    parts.append('<hr>Check all alerts in <a href="{}/web">Snoozeweb</a>'.format(website))

                return {'body': {'content': '{}<br>{}'.format(TeamsPlugin.BOT_MARKER, '<br>'.join(parts)), 'contentType': 'html'}}

        for msg_item in message['messages']:
            msg = msg_item.get('msg', {})
            record = msg.get('record', {})
            if not record:
                continue
            host = record.get('host', 'Unknown')
            source = record.get('source', 'Unknown')
            process = record.get('process', 'Unknown')
            severity = record.get('severity', 'Unknown')
            alert_message = record.get('message', '')
            record_hash = record.get('hash', '')
            timestamp = SnoozeBotPlugin.date_regex.sub(
                lambda m: parser.parse(m.group()).astimezone().strftime(self.date_format),
                record.get('timestamp', str(datetime.now().astimezone()))
            )

            link = '<a href="{}/web/?#/record?tab=All&s=hash%3D{}">{}</a>'.format(website, record_hash, host)
            escalation = ' (e)' if msg.get('threads') else ''

            line = '<b>{}{}</b> [{}] <i>{}</i>'.format(link, escalation, source, process)
            if severity:
                line += ' - {}'.format(severity)
            if alert_message:
                line += '<br>{}'.format(alert_message)
            if msg.get('from'):
                notif = 'From <b>{}</b>'.format(msg.get('from'))
                if msg.get('from_msg'):
                    notif += ': {}'.format(msg.get('from_msg'))
                line += ' ({})'.format(notif)
            if msg.get('message'):
                line += '<br><i>{}</i>'.format(msg.get('message'))
            parts.append(line)

        if message.get('footer'):
            parts.append('Check all alerts in <a href="{}/web">Snoozeweb</a>'.format(website))

        return {'body': {'content': '{}<br>{}'.format(TeamsPlugin.BOT_MARKER, '<br>'.join(parts)), 'contentType': 'html'}}


class TeamsPoller(threading.Thread):

    def __init__(self, plugin):
        super(TeamsPoller, self).__init__(daemon=True)
        self.plugin = plugin
        self._stop_event = threading.Event()
        self._checkpoints = {}
        self._lookback = timedelta(seconds=self.plugin.poll_lookback_seconds)
        self._recent_ids_limit = 2000
        self._global_recent_ids = []
        self._global_recent_id_set = set()

    def _get_checkpoint(self, resource):
        if resource in self._checkpoints:
            return self._checkpoints[resource]
        checkpoint = {
            'since': datetime.now().astimezone() - self._lookback,
            'recent_ids': [],
            'recent_id_set': set(),
        }
        self._checkpoints[resource] = checkpoint
        return checkpoint

    def _remember_id(self, checkpoint, message_id):
        if message_id in checkpoint['recent_id_set']:
            return
        checkpoint['recent_ids'].append(message_id)
        checkpoint['recent_id_set'].add(message_id)
        if len(checkpoint['recent_ids']) > self._recent_ids_limit:
            old = checkpoint['recent_ids'].pop(0)
            checkpoint['recent_id_set'].discard(old)

    def _remember_global_id(self, message_id):
        if message_id in self._global_recent_id_set:
            return
        self._global_recent_ids.append(message_id)
        self._global_recent_id_set.add(message_id)
        if len(self._global_recent_ids) > self._recent_ids_limit:
            old = self._global_recent_ids.pop(0)
            self._global_recent_id_set.discard(old)

    def _parse_graph_datetime(self, text):
        if not text:
            return None
        try:
            return parser.parse(text)
        except Exception:
            return None

    def _poll_resource(self, resource):
        checkpoint = self._get_checkpoint(resource)
        roots = self.plugin.fetch_messages(resource)
        roots = sorted(roots, key=lambda m: m.get('createdDateTime', ''))
        for graph_message in roots:
            self._process_graph_message(resource, graph_message, checkpoint)
            root_id = graph_message.get('id')
            if not root_id:
                continue
            try:
                replies = self.plugin.fetch_replies(resource, root_id)
            except Exception as e:
                LOG.debug('Unable to fetch replies for %s in %s: %s', root_id, resource, e)
                continue
            replies = sorted(replies, key=lambda m: m.get('createdDateTime', ''))
            for reply in replies:
                self._process_graph_message(resource, reply, checkpoint)

    def _process_graph_message(self, resource, graph_message, checkpoint):
        message_id = graph_message.get('id')
        if not message_id:
            return
        if message_id in self._global_recent_id_set:
            return
        if message_id in checkpoint['recent_id_set']:
            return
        created = self._parse_graph_datetime(graph_message.get('createdDateTime'))
        if created and created <= checkpoint['since']:
            self._remember_id(checkpoint, message_id)
            self._remember_global_id(message_id)
            return
        self._remember_id(checkpoint, message_id)
        self._remember_global_id(message_id)
        checkpoint['since'] = max(checkpoint['since'], created) if created else checkpoint['since']
        if self.plugin.is_self_message(graph_message):
            return
        msg = self.plugin.normalize_incoming_message(graph_message)
        if not getattr(msg, 'text', '').strip():
            return
        try:
            response_text = self.plugin.process_user_message(msg)
            self.plugin.reply_to_polled_message(response_text, resource, graph_message)
        except Exception as e:
            LOG.exception("Failed to process polled message %s on %s: %s", message_id, resource, e)

    def run(self):
        LOG.info("Starting Teams polling worker (interval=%ss)", self.plugin.poll_interval_seconds)
        while not self._stop_event.is_set():
            resources = self.plugin.get_poll_resources()
            for resource in resources:
                if self._stop_event.is_set():
                    break
                try:
                    self._poll_resource(resource)
                except Exception as e:
                    LOG.warning("Polling failed for %s: %s", resource, e)
            self._stop_event.wait(self.plugin.poll_interval_seconds)
        LOG.info("Teams polling worker stopped")

    def kill(self):
        self._stop_event.set()
        self.join(timeout=5)

class TeamsBot():

    def __init__(self):
        self.snoozebot = SnoozeBot('SNOOZE_TEAMS_PATH', 'teamsbot.yaml', 'TeamsPlugin')
        client_id = self.snoozebot.config.get('client_id')
        client_secret = self.snoozebot.config.get('client_secret')
        tenant_id = self.snoozebot.config.get('tenant_id')
        scopes = [
            'offline_access',
            'ChannelMessage.Send',
            'ChannelMessage.Read.All',
            'Chat.ReadBasic',
            'Team.ReadBasic.All',
            'Channel.ReadBasic.All'
        ]
        credentials = (client_id, client_secret)
        protocol = MSGraphProtocol(api_version='beta')
        account = Account(credentials, tenant_id=tenant_id, protocol=protocol)
        expected_scopes = set(scopes)
        missing_scopes = set(expected_scopes)
        if account.is_authenticated:
            try:
                token_data = {}
                token_backend = account.con.token_backend
                if hasattr(token_backend, 'get_access_token'):
                    token_data = token_backend.get_access_token() or {}
                else:
                    if hasattr(token_backend, 'load_token'):
                        token_backend.load_token()
                    token_data = getattr(token_backend, 'token', {}) or {}
                token_scope_value = token_data.get('scope', '')
                if isinstance(token_scope_value, list):
                    token_scopes = set(token_scope_value)
                else:
                    token_scopes = set((token_scope_value or '').split())
                missing_scopes = set(
                    scope for scope in expected_scopes
                    if scope != 'offline_access' and scope not in token_scopes and 'https://graph.microsoft.com/{}'.format(scope) not in token_scopes
                )
            except Exception as e:
                LOG.warning('Could not inspect current token scopes: %s', e)
        if (not account.is_authenticated) or missing_scopes:
            if missing_scopes:
                LOG.warning('Token is missing scopes (%s). Triggering re-authentication.', ', '.join(sorted(missing_scopes)))
            account.authenticate(scopes=scopes, redirect_uri='https://localhost')
        self.snoozebot.plugin.driver = account.teams()
        try:
            me = account.con.get('https://graph.microsoft.com/v1.0/me').json()
            self.snoozebot.plugin.self_user_id = me.get('id', '')
            self.snoozebot.plugin.self_user_name = me.get('displayName', '')
            LOG.info('Authenticated Teams identity: %s (%s)', self.snoozebot.plugin.self_user_name, self.snoozebot.plugin.self_user_id)
        except Exception as e:
            LOG.warning('Could not fetch authenticated Teams identity: %s', e)

def main():
    TeamsBot().snoozebot.plugin.serve()

if __name__ == '__main__':
    main()
