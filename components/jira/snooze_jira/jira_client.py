import logging
import time
from base64 import b64encode

import requests
from requests import HTTPError

LOG = logging.getLogger("snooze.jira")


class JiraClient:
    """Wrapper around the JIRA Cloud REST API v3."""

    def __init__(self, base_url, email, api_token, verify_ssl=True):
        self.base_url = base_url.rstrip('/')
        self.verify_ssl = verify_ssl
        self.session = requests.Session()
        credentials = b64encode(f"{email}:{api_token}".encode()).decode()
        self.session.headers.update({
            'Authorization': f'Basic {credentials}',
            'Content-Type': 'application/json',
            'Accept': 'application/json',
        })

    def _request(self, method, path, json=None, retries=3):
        """Execute an HTTP request with retry logic."""
        url = f"{self.base_url}/rest/api/3{path}"
        for attempt in range(retries):
            try:
                resp = self.session.request(method, url, json=json, verify=self.verify_ssl)
                resp.raise_for_status()
                if resp.status_code == 204 or not resp.content:
                    return {}
                return resp.json()
            except HTTPError as e:
                response = e.response
                status = getattr(response, 'status_code', 'unknown')
                body = (getattr(response, 'text', '') or '').strip()
                jira_errors = ''
                try:
                    error_json = response.json() if response is not None else {}
                    error_messages = error_json.get('errorMessages', [])
                    field_errors = error_json.get('errors', {})
                    if error_messages or field_errors:
                        jira_errors = f" errorMessages={error_messages} fieldErrors={field_errors}"
                except Exception:
                    pass
                LOG.warning(
                    "JIRA API request failed (attempt %d/%d): HTTP %s %s%s body=%s",
                    attempt + 1,
                    retries,
                    status,
                    method,
                    jira_errors,
                    body,
                )
                if attempt < retries - 1:
                    time.sleep(1)
                else:
                    raise
            except Exception as e:
                LOG.warning("JIRA API request failed (attempt %d/%d): %s", attempt + 1, retries, e)
                if attempt < retries - 1:
                    time.sleep(1)
                else:
                    raise
        return {}

    def create_issue(self, project_key, issue_type, summary, description_adf,
                     priority=None, labels=None, extra_fields=None, issue_type_id=None):
        """Create a new JIRA issue.

        Args:
            project_key: JIRA project key (e.g. 'OPS')
            issue_type: Issue type name (e.g. 'Task', 'Bug')
            summary: Issue summary string
            description_adf: Description in Atlassian Document Format (dict)
            priority: Priority name (e.g. 'High', 'Medium')
            labels: List of label strings
            extra_fields: Dict of additional fields to set
            issue_type_id: Jira issue type ID (e.g. '10001'). Overrides issue_type when set

        Returns:
            dict with 'id', 'key', 'self' of the created issue
        """
        fields = {
            'project': {'key': project_key},
            'summary': summary,
            'description': description_adf,
        }

        if issue_type_id:
            fields['issuetype'] = {'id': str(issue_type_id)}
        else:
            fields['issuetype'] = {'name': issue_type}
        if priority:
            fields['priority'] = {'name': priority}
        if labels:
            fields['labels'] = labels
        if extra_fields:
            fields.update(extra_fields)

        payload = {'fields': fields}
        LOG.debug("Creating JIRA issue: %s", payload)
        try:
            result = self._request('POST', '/issue', json=payload)
        except HTTPError as e:
            if self._priority_error_requires_string(e) and priority:
                # Some Jira instances require priority as a string instead of {"name": ...}.
                fallback_payload = {'fields': dict(fields)}
                fallback_payload['fields']['priority'] = priority
                LOG.info("Retrying JIRA issue creation with string priority format")
                result = self._request('POST', '/issue', json=fallback_payload)
            else:
                raise
        LOG.info("Created JIRA issue: %s", result.get('key'))
        return result

    @staticmethod
    def _priority_error_requires_string(error):
        response = getattr(error, 'response', None)
        if response is None:
            return False
        try:
            errors = (response.json() or {}).get('errors', {})
        except Exception:
            return False
        priority_error = str(errors.get('priority', '')).casefold()
        return bool(priority_error and ('string' in priority_error or 'cha' in priority_error))

    def add_comment(self, issue_key, comment_text):
        """Add a comment to an existing JIRA issue.

        Args:
            issue_key: The issue key (e.g. 'OPS-123')
            comment_text: Plain text comment to add

        Returns:
            dict with comment details
        """
        payload = {
            'body': self._text_to_adf(comment_text),
        }
        LOG.debug("Adding comment to %s: %s", issue_key, comment_text)
        return self._request('POST', f'/issue/{issue_key}/comment', json=payload)

    def transition_issue(self, issue_key, transition_id, comment=None):
        """Transition a JIRA issue to a different status.

        Args:
            issue_key: The issue key (e.g. 'OPS-123')
            transition_id: The transition ID to apply
            comment: Optional comment to add with the transition
        """
        payload = {
            'transition': {'id': str(transition_id)},
        }
        if comment:
            payload['update'] = {
                'comment': [{
                    'add': {
                        'body': self._text_to_adf(comment),
                    }
                }]
            }
        LOG.debug("Transitioning %s with transition %s", issue_key, transition_id)
        return self._request('POST', f'/issue/{issue_key}/transitions', json=payload)

    def get_issue(self, issue_key):
        """Get issue details.

        Args:
            issue_key: The issue key (e.g. 'OPS-123')

        Returns:
            dict with issue fields
        """
        return self._request('GET', f'/issue/{issue_key}')

    def get_transitions(self, issue_key):
        """Get available transitions for an issue.

        Args:
            issue_key: The issue key (e.g. 'OPS-123')

        Returns:
            list of transition dicts with 'id', 'name', 'to' fields
        """
        result = self._request('GET', f'/issue/{issue_key}/transitions')
        return result.get('transitions', [])

    def search_issues(self, jql, fields=None, max_results=50):
        """Search for issues using JQL.

        Args:
            jql: JQL query string
            fields: List of field names to return (default: all navigable fields)
            max_results: Maximum number of results (default 50)

        Returns:
            list of issue dicts
        """
        payload = {
            'jql': jql,
            'maxResults': max_results,
        }
        if fields:
            payload['fields'] = fields
        result = self._request('POST', '/search/jql', json=payload)
        return result.get('issues', [])

    def find_user_by_email(self, email):
        """Look up a JIRA user's accountId by email address.

        Uses the /rest/api/3/user/search endpoint with the email as query.

        Args:
            email: Email address to search for

        Returns:
            accountId string if exactly one match is found, None otherwise
        """
        url = f"{self.base_url}/rest/api/3/user/search"
        try:
            resp = self.session.get(url, params={'query': email}, verify=self.verify_ssl)
            resp.raise_for_status()
            users = resp.json()
            if users and len(users) == 1:
                account_id = users[0].get('accountId')
                LOG.info("Resolved email '%s' to accountId '%s'", email, account_id)
                return account_id
            elif users and len(users) > 1:
                # Try exact match on emailAddress field
                for user in users:
                    if user.get('emailAddress', '').lower() == email.lower():
                        account_id = user.get('accountId')
                        LOG.info("Resolved email '%s' to accountId '%s' (exact match)", email, account_id)
                        return account_id
                # Fall back to first result
                account_id = users[0].get('accountId')
                LOG.warning("Multiple users found for '%s', using first: '%s'", email, account_id)
                return account_id
            else:
                LOG.warning("No JIRA user found for email '%s'", email)
                return None
        except Exception as e:
            LOG.exception("Failed to search for user by email '%s': %s", email, e)
            return None

    @staticmethod
    def _text_to_adf(text):
        """Convert plain text to Atlassian Document Format (ADF).

        Args:
            text: Plain text string

        Returns:
            dict in ADF format
        """
        paragraphs = text.split('\n')
        content = []
        for paragraph in paragraphs:
            if paragraph.strip():
                content.append({
                    'type': 'paragraph',
                    'content': [{
                        'type': 'text',
                        'text': paragraph,
                    }]
                })
            else:
                content.append({
                    'type': 'paragraph',
                    'content': [],
                })
        return {
            'type': 'doc',
            'version': 1,
            'content': content,
        }

    @staticmethod
    def build_description_adf(record, snooze_url=''):
        """Build a rich ADF description from a Snooze alert record.

        Args:
            record: Snooze alert record dict
            snooze_url: Base URL for Snooze Web UI

        Returns:
            dict in ADF format
        """
        content = []

        # Header
        content.append({
            'type': 'heading',
            'attrs': {'level': 3},
            'content': [{'type': 'text', 'text': 'Snooze Alert'}],
        })

        # Alert fields table
        fields = [
            ('Host', record.get('host', 'Unknown')),
            ('Source', record.get('source', 'Unknown')),
            ('Process', record.get('process', 'Unknown')),
            ('Severity', record.get('severity', 'Unknown')),
            ('Timestamp', record.get('timestamp', 'Unknown')),
        ]

        for key, value in fields:
            content.append({
                'type': 'paragraph',
                'content': [
                    {'type': 'text', 'text': f'{key}: ', 'marks': [{'type': 'strong'}]},
                    {'type': 'text', 'text': str(value)},
                ],
            })

        # Message
        message = record.get('message', '')
        if message:
            content.append({
                'type': 'paragraph',
                'content': [
                    {'type': 'text', 'text': 'Message: ', 'marks': [{'type': 'strong'}]},
                    {'type': 'text', 'text': message},
                ],
            })

        # Snooze link
        if snooze_url:
            record_hash = record.get('hash', '')
            link_url = f"{snooze_url}/web/?#/record?tab=All&s=hash%3D{record_hash}"
            content.append({
                'type': 'paragraph',
                'content': [{
                    'type': 'text',
                    'text': 'View in Snooze',
                    'marks': [{'type': 'link', 'attrs': {'href': link_url}}],
                }],
            })

        return {
            'type': 'doc',
            'version': 1,
            'content': content,
        }
