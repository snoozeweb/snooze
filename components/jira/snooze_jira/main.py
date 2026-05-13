import yaml
import os
import logging
import re
import threading
import falcon
from datetime import datetime
from pathlib import Path
from string import Template
from snooze_client import Snooze
from snooze_jira.jira_client import JiraClient

from waitress.adjustments import Adjustments
from waitress.server import TcpWSGIServer

LOG = logging.getLogger("snooze.jira")


class SnoozeBot:
    """Configuration loader following the same pattern as other Snooze output plugins."""

    def __init__(self, env_name, file_name, plugin_class='JiraPlugin'):
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


class JiraPlugin:
    """Snooze output plugin that creates and updates JIRA tickets from alerts."""

    def __init__(self, config):
        self.config = config
        self.address = self.config.get('listening_address', '0.0.0.0')
        self.port = self.config.get('listening_port', 5203)
        self.snooze_url = self.config.get('snooze_url', 'http://localhost:5200')
        if self.snooze_url.endswith('/'):
            self.snooze_url = self.snooze_url[:-1]
        self.message_limit = self.config.get('message_limit', 10)

        # JIRA configuration
        jira_url = self.config.get('jira_url', '')
        jira_email = self.config.get('jira_email', '')
        jira_api_token = self.config.get('jira_api_token', '')
        verify_ssl = self.config.get('ssl_verify', True)

        if not all([jira_url, jira_email, jira_api_token]):
            LOG.error("Missing required JIRA configuration: jira_url, jira_email, jira_api_token")

        self.jira = JiraClient(jira_url, jira_email, jira_api_token, verify_ssl=verify_ssl)
        self.client = Snooze()

        # Default issue settings
        self.default_project_key = self.config.get('project_key', '')
        self.default_issue_type = self.config.get('issue_type', 'Task')
        self.default_issue_type_id = self.config.get('issue_type_id', '')
        self.default_priority = self.config.get('priority', 'Medium')
        self.default_labels = self.config.get('labels', ['snooze'])
        self.summary_template = self.config.get('summary_template', '[${severity}] ${host} - ${message}')
        self.description_template = self.config.get('description_template', '')
        self.extra_fields = self.config.get('extra_fields', {})

        # Priority mapping: maps Snooze severity to JIRA priority name
        self.priority_mapping = self.config.get('priority_mapping', {
            'emergency': 'Critical',
            'critical': 'High',
            'warning': 'Medium',
            'minor': 'Low',
            'info': 'Lowest',
        })

        # Custom JIRA fields
        self.default_assignee = self.config.get('assignee', '')
        self.default_reporter = self.config.get('reporter', '')
        self.custom_fields = self.config.get('custom_fields', {})

        # Reopen configuration: optionally reopen closed tickets on re-escalation
        self.reopen_closed = self.config.get('reopen_closed', False)
        self.reopen_status_name = self.config.get('reopen_status_name', 'To Do')

        # Initial status: optionally transition newly created issues to a specific status
        self.initial_status = self.config.get('initial_status', '')

        # Custom field to store the Snooze alert URL on the JIRA issue (e.g. 'customfield_10500')
        self.alert_hash_custom_field = self.config.get('alert_hash_custom_field', '')

        # Polling configuration for bidirectional status sync
        # Enabled by default, but requires alert_hash_custom_field to be set
        self.poll_enabled = self.config.get('poll_enabled', True)
        self.poll_interval = self.config.get('poll_interval', 300)
        self.poll_jql = self.config.get('poll_jql', '')

    def process_alert(self, request, medias):
        """Entry point for processing alert webhooks."""
        if not isinstance(medias, list):
            medias = [medias]
        response = self.process_records(request, medias)
        LOG.debug("Response: %s", response)
        return response

    def process_records(self, req, medias):
        """Process a list of alert records and create/update JIRA tickets.

        For each alert:
        - If a JIRA ticket already exists (from previous webhook response), add a comment
        - Otherwise, create a new JIRA issue

        Returns:
            dict mapping record hashes to their JIRA issue info for webhook response injection
        """
        return_value = {}
        action_name = req.params.get('snooze_action_name', 'unknown_action')

        for req_media in medias[:self.message_limit]:
            record = req_media.get('alert', {})
            message = req_media.get('message')
            notification_from = record.get('notification_from')
            record_hash = record.get('hash', '')

            LOG.debug('Received record: %s', record)

            # Check if we already created a ticket for this alert (via previous webhook responses)
            existing_issue = self._find_existing_issue(record, action_name)

            if existing_issue:
                # Add a comment to the existing ticket
                comment = self._build_comment(record, message, notification_from)
                try:
                    self.jira.add_comment(existing_issue, comment)
                    LOG.info("Added comment to existing issue %s for record %s", existing_issue, record_hash)

                    # Optionally reopen the ticket if it is closed/done
                    if self.reopen_closed:
                        self._reopen_issue_if_closed(existing_issue)

                except Exception as e:
                    LOG.exception("Failed to update %s: %s", existing_issue, e)
                return_value[record_hash] = {'issue_key': existing_issue}
            else:
                # Create a new JIRA issue
                project_key = req_media.get('project_key', self.default_project_key)
                issue_type = self.default_issue_type
                issue_type_id = self.default_issue_type_id

                payload_issue_type = req_media.get('issue_type')
                payload_issue_type_id = req_media.get('issue_type_id')

                # Precedence: payload issue_type_id > payload issue_type > config issue_type_id > config issue_type
                if payload_issue_type not in (None, ''):
                    issue_type = payload_issue_type
                    issue_type_id = ''

                if payload_issue_type_id not in (None, ''):
                    issue_type_id = payload_issue_type_id

                labels = req_media.get('labels', self.default_labels)
                extra_fields = req_media.get('extra_fields', self.extra_fields)

                # Resolve priority: payload override > mapping from severity > default
                priority = req_media.get('priority')
                if not priority:
                    severity = record.get('severity', '').lower()
                    priority = self.priority_mapping.get(severity, self.default_priority)

                # Resolve assignee, reporter (payload override > config default)
                assignee = req_media.get('assignee', self.default_assignee)
                reporter = req_media.get('reporter', self.default_reporter)

                # Merge custom fields: config defaults, then payload overrides
                merged_custom_fields = dict(self.custom_fields)
                merged_custom_fields.update(req_media.get('custom_fields', {}))

                if not project_key:
                    LOG.error("No project_key specified for record %s, skipping", record_hash)
                    continue

                summary = self._format_summary(record)

                # Build description: use template if configured, otherwise use default rich ADF
                if self.description_template:
                    description_adf = self._format_description(record)
                else:
                    description_adf = JiraClient.build_description_adf(record, self.snooze_url)

                # Append custom message if provided
                if message:
                    description_adf['content'].append({
                        'type': 'paragraph',
                        'content': [
                            {'type': 'text', 'text': 'Custom message: ', 'marks': [{'type': 'strong'}]},
                            {'type': 'text', 'text': message},
                        ],
                    })

                # Append notification origin if present
                if notification_from:
                    notif_name = notification_from.get('name', 'anonymous')
                    notif_message = notification_from.get('message', '')
                    notif_text = f'Notified by {notif_name}'
                    if notif_message:
                        notif_text += f': {notif_message}'
                    description_adf['content'].append({
                        'type': 'paragraph',
                        'content': [{'type': 'text', 'text': notif_text}],
                    })

                # Build extra fields with assignee, reporter, and custom fields
                combined_extra = dict(extra_fields)
                if assignee:
                    user_field = self._resolve_user_field(assignee)
                    if user_field:
                        combined_extra['assignee'] = user_field
                if reporter:
                    user_field = self._resolve_user_field(reporter)
                    if user_field:
                        combined_extra['reporter'] = user_field
                combined_extra.update(merged_custom_fields)

                # Store the Snooze alert URL in the configured custom field
                if self.alert_hash_custom_field and record_hash:
                    snooze_link = f"{self.snooze_url}/web/?#/record?tab=All&s=hash%3D{record_hash}"
                    combined_extra[self.alert_hash_custom_field] = snooze_link

                try:
                    result = self.jira.create_issue(
                        project_key=project_key,
                        issue_type=issue_type,
                        issue_type_id=issue_type_id,
                        summary=summary,
                        description_adf=description_adf,
                        priority=priority,
                        labels=labels,
                        extra_fields=combined_extra,
                    )
                    issue_key = result.get('key', '')
                    LOG.info("Created JIRA issue %s for record %s", issue_key, record_hash)

                    # Transition to initial status if configured
                    initial_status = req_media.get('initial_status', self.initial_status)
                    if issue_key and initial_status:
                        self._transition_to_status(issue_key, initial_status)

                    return_value[record_hash] = {'issue_key': issue_key}
                except Exception as e:
                    LOG.exception("Failed to create JIRA issue for record %s: %s", record_hash, e)

        # For alerts beyond the message limit, log a warning
        if len(medias) > self.message_limit:
            LOG.warning(
                "Received %d alerts but message_limit is %d. %d alerts were not processed.",
                len(medias), self.message_limit, len(medias) - self.message_limit
            )

        return return_value

    def _find_existing_issue(self, record, action_name):
        """Look for an existing JIRA issue key in the record's webhook responses.

        Args:
            record: The alert record dict
            action_name: The name of the current action

        Returns:
            The issue key string if found, None otherwise
        """
        for action_result in record.get('snooze_webhook_responses', []):
            if action_result.get('action_name') == action_name:
                issue_key = action_result.get('content', {}).get('issue_key')
                if issue_key:
                    return issue_key
        return None

    @staticmethod
    def _extract_hash_from_field(value):
        """Extract the alert hash from a custom field value.

        The field may contain a Snooze URL (e.g. https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dabc123)
        or a plain hash string.

        Args:
            value: The custom field value string

        Returns:
            The extracted alert hash, or the original value if no URL pattern is found
        """
        if not value:
            return ''
        match = re.search(r'hash(?:%3D|=)([^&\s]+)', value)
        if match:
            return match.group(1)
        return value

    def _reopen_issue_if_closed(self, issue_key):
        """Reopen a JIRA issue if it is in a done/closed status category.

        Looks up the issue's current status category. If it is 'done' (closed/resolved),
        finds a transition that moves it to the configured reopen status and applies it.

        Args:
            issue_key: The JIRA issue key (e.g. 'OPS-42')
        """
        try:
            issue = self.jira.get_issue(issue_key)
            status_category = issue.get('fields', {}).get('status', {}).get('statusCategory', {}).get('key', '')
            if status_category == 'done':
                self._transition_to_status(
                    issue_key, self.reopen_status_name,
                    comment='Reopened by Snooze due to re-escalation',
                )
        except Exception as e:
            LOG.exception("Failed to reopen issue %s: %s", issue_key, e)

    def _transition_to_status(self, issue_key, target_status_name, comment=None):
        """Transition a JIRA issue to a target status by name.

        Looks up available transitions and finds one whose destination status
        matches the target name (case-insensitive). Falls back to any non-done
        transition if no exact match is found.

        Args:
            issue_key: The JIRA issue key (e.g. 'OPS-42')
            target_status_name: Desired target status name (e.g. 'In Progress', 'To Do')
            comment: Optional comment to include with the transition
        """
        try:
            transitions = self.jira.get_transitions(issue_key)
            transition = None

            # Try exact match on target status name
            for t in transitions:
                to_name = t.get('to', {}).get('name', '')
                if to_name.lower() == target_status_name.lower():
                    transition = t
                    break

            # Fallback: pick the first transition that is not to a 'done' category
            if not transition:
                for t in transitions:
                    to_category = t.get('to', {}).get('statusCategory', {}).get('key', '')
                    if to_category != 'done':
                        transition = t
                        break

            if transition:
                self.jira.transition_issue(issue_key, transition['id'], comment=comment)
                LOG.info("Transitioned issue %s to '%s' via transition '%s'",
                         issue_key, target_status_name, transition.get('name'))
            else:
                LOG.warning("Could not find a transition to '%s' for %s", target_status_name, issue_key)
        except Exception as e:
            LOG.exception("Failed to transition issue %s to '%s': %s", issue_key, target_status_name, e)

    def _resolve_user_field(self, value):
        """Resolve a user identifier to a JIRA user field dict.

        If the value contains '@', it is treated as an email address and
        resolved to an accountId via the JIRA user search API.
        Otherwise, it is used directly as a JIRA account ID.

        Results are cached to avoid repeated API calls.

        Args:
            value: An email address or JIRA account ID string

        Returns:
            dict suitable for JIRA assignee/reporter fields, or None if email lookup fails
        """
        if '@' in value:
            # Check cache first
            if not hasattr(self, '_user_cache'):
                self._user_cache = {}
            if value in self._user_cache:
                return self._user_cache[value]
            account_id = self.jira.find_user_by_email(value)
            if account_id:
                result = {'id': account_id}
                self._user_cache[value] = result
                return result
            LOG.warning("Could not resolve email '%s' to a JIRA accountId, skipping user field", value)
            return None
        return {'id': value}

    def _format_summary(self, record):
        """Format the JIRA issue summary from the alert record using the configured template.

        Args:
            record: The alert record dict

        Returns:
            Formatted summary string, truncated to 255 chars (JIRA limit)
        """
        template = Template(self.summary_template)
        try:
            summary = template.safe_substitute(
                severity=record.get('severity', 'Unknown'),
                host=record.get('host', 'Unknown'),
                source=record.get('source', 'Unknown'),
                process=record.get('process', 'Unknown'),
                message=record.get('message', 'No message'),
                timestamp=record.get('timestamp', ''),
            )
        except Exception:
            summary = '[{}] {} - {}'.format(
                record.get('severity', 'Unknown'),
                record.get('host', 'Unknown'),
                record.get('message', 'No message'),
            )
        # JIRA summary field has a 255 character limit
        return summary[:255]

    def _format_description(self, record):
        """Format the JIRA issue description from the alert record using the configured template.

        Supports the same ${variable} syntax as summary_template. The rendered text
        is converted to ADF paragraphs (one per line).

        Available variables: ${severity}, ${host}, ${source}, ${process}, ${message},
        ${timestamp}, ${hash}, ${snooze_url}

        Args:
            record: The alert record dict

        Returns:
            dict in Atlassian Document Format (ADF)
        """
        template = Template(self.description_template)
        try:
            text = template.safe_substitute(
                severity=record.get('severity', 'Unknown'),
                host=record.get('host', 'Unknown'),
                source=record.get('source', 'Unknown'),
                process=record.get('process', 'Unknown'),
                message=record.get('message', 'No message'),
                timestamp=record.get('timestamp', ''),
                hash=record.get('hash', ''),
                snooze_url=self.snooze_url,
            )
        except Exception:
            text = record.get('message', 'No message')
        return JiraClient._text_to_adf(text)

    def _build_comment(self, record, message=None, notification_from=None):
        """Build a comment string for an existing JIRA issue.

        Args:
            record: The alert record dict
            message: Optional custom message
            notification_from: Optional notification origin dict

        Returns:
            Comment text string
        """
        timestamp = record.get('timestamp', str(datetime.now().astimezone()))
        parts = [f"Re-escalation at {timestamp}"]

        if notification_from:
            notif_name = notification_from.get('name', 'anonymous')
            notif_message = notification_from.get('message', '')
            parts.append(f"From {notif_name}")
            if notif_message:
                parts.append(notif_message)

        parts.append(f"Host: {record.get('host', 'Unknown')}")
        parts.append(f"Severity: {record.get('severity', 'Unknown')}")
        parts.append(f"Message: {record.get('message', 'No message')}")

        if message:
            parts.append(f"Custom message: {message}")

        return '\n'.join(parts)

    def _start_poller(self):
        """Start the background JIRA poller thread if enabled."""
        if not self.poll_enabled:
            return
        if not self.alert_hash_custom_field:
            LOG.error("poll_enabled is True but alert_hash_custom_field is not configured, skipping poller")
            return
        self._tracked_issues = {}  # {issue_key: alert_hash}
        self._poll_stop = threading.Event()
        self._poll_thread = threading.Thread(target=self._poll_loop, daemon=True)
        self._poll_thread.start()
        LOG.info("JIRA poller started (interval=%ds)", self.poll_interval)

    def _poll_loop(self):
        """Background loop that polls JIRA for ticket status changes."""
        while not self._poll_stop.wait(self.poll_interval):
            try:
                self._poll_cycle()
            except Exception as e:
                LOG.exception("JIRA poll cycle failed: %s", e)

    def _poll_cycle(self):
        """Single poll cycle: find open issues, detect newly-closed ones, update Snooze."""
        # Build JQL to find open issues with the alert hash field set
        if self.poll_jql:
            jql = self.poll_jql
        else:
            # Extract numeric field ID from customfield_XXXXX for cf[] syntax
            field_id = self.alert_hash_custom_field
            if field_id.startswith('customfield_'):
                cf_num = field_id[len('customfield_'):]
                jql = f'cf[{cf_num}] is not EMPTY AND statusCategory != Done'
            else:
                jql = f'"{field_id}" is not EMPTY AND statusCategory != Done'

        fields = [self.alert_hash_custom_field, 'status']
        issues = self.jira.search_issues(jql, fields=fields)

        current_open = {}
        for issue in issues:
            issue_key = issue.get('key', '')
            field_value = issue.get('fields', {}).get(self.alert_hash_custom_field, '')
            alert_hash = self._extract_hash_from_field(field_value)
            if issue_key and alert_hash:
                current_open[issue_key] = alert_hash

        LOG.debug("Poll cycle: %d open issues with alert hash field", len(current_open))

        # Detect issues that were tracked but are no longer open -> closed in JIRA
        closed_keys = set(self._tracked_issues.keys()) - set(current_open.keys())
        for issue_key in closed_keys:
            alert_hash = self._tracked_issues[issue_key]
            LOG.info("JIRA issue %s no longer open, closing Snooze alert (hash=%s)", issue_key, alert_hash)
            self._close_snooze_alert(alert_hash, issue_key)

        # Update tracked set to the current open issues
        self._tracked_issues = current_open

    def _close_snooze_alert(self, alert_hash, issue_key):
        """Close the Snooze alert corresponding to a resolved JIRA ticket."""
        try:
            records = self.client.record(['=', 'hash', alert_hash])
            if not records:
                LOG.warning("No Snooze record found for hash '%s' (JIRA issue %s)", alert_hash, issue_key)
                return
            for record in records:
                uid = record.get('uid')
                if not uid:
                    continue
                self.client.comment_batch([{
                    'type': 'close',
                    'record_uid': uid,
                    'name': 'jira',
                    'method': 'jira',
                    'message': f'Closed: JIRA ticket {issue_key} resolved',
                }])
                LOG.info("Closed Snooze alert uid=%s (hash=%s) due to JIRA %s", uid, alert_hash, issue_key)
        except Exception as e:
            LOG.exception("Failed to close Snooze alert for hash '%s': %s", alert_hash, e)

    def serve(self):
        """Start the Falcon + Waitress HTTP server."""
        self._start_poller()
        self.app = falcon.App()
        self.app.add_route('/alert', AlertRoute(self))
        wsgi_options = Adjustments(host=self.address, port=self.port)
        httpd = TcpWSGIServer(self.app, adj=wsgi_options)
        LOG.info("Serving on port %s...", str(self.port))
        httpd.run()


class AlertRoute:
    """Falcon route handling POST /alert requests from SnoozeWeb."""

    def __init__(self, plugin):
        self.plugin = plugin

    def on_post(self, req, resp):
        medias = req.media
        if not isinstance(medias, list):
            medias = [medias]
        response = self.plugin.process_records(req, medias)
        LOG.debug("Response: %s", response)
        resp.status = falcon.HTTP_200
        if response:
            resp.content_type = falcon.MEDIA_JSON
            resp.media = response


class JiraBot:
    """Main entry point that loads configuration and starts the JIRA plugin daemon."""

    def __init__(self):
        self.snoozebot = SnoozeBot('SNOOZE_JIRA_PATH', 'jira.yaml', 'JiraPlugin')


def main():
    JiraBot().snoozebot.plugin.serve()


if __name__ == '__main__':
    main()
