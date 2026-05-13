import json
import pytest
from unittest.mock import patch, MagicMock
from requests import HTTPError

from snooze_jira.jira_client import JiraClient


class TestJiraClient:
    """Tests for the JIRA REST API client wrapper."""

    def setup_method(self):
        self.client = JiraClient(
            base_url='https://test.atlassian.net',
            email='test@example.com',
            api_token='test-token',
        )

    def test_text_to_adf_simple(self):
        result = JiraClient._text_to_adf('Hello world')
        assert result == {
            'type': 'doc',
            'version': 1,
            'content': [{
                'type': 'paragraph',
                'content': [{'type': 'text', 'text': 'Hello world'}],
            }],
        }

    def test_text_to_adf_multiline(self):
        result = JiraClient._text_to_adf('Line 1\nLine 2\nLine 3')
        assert result['type'] == 'doc'
        assert len(result['content']) == 3
        assert result['content'][0]['content'][0]['text'] == 'Line 1'
        assert result['content'][1]['content'][0]['text'] == 'Line 2'
        assert result['content'][2]['content'][0]['text'] == 'Line 3'

    def test_text_to_adf_with_blank_lines(self):
        result = JiraClient._text_to_adf('Before\n\nAfter')
        assert len(result['content']) == 3
        assert result['content'][1]['content'] == []

    def test_build_description_adf(self):
        record = {
            'host': 'web01',
            'source': 'nagios',
            'process': 'httpd',
            'severity': 'critical',
            'timestamp': '2026-02-16T10:00:00+0000',
            'message': 'HTTP service is down',
            'hash': 'abc123',
        }
        result = JiraClient.build_description_adf(record, 'https://snooze.example.com')

        assert result['type'] == 'doc'
        assert result['version'] == 1

        # Check heading
        assert result['content'][0]['type'] == 'heading'
        assert result['content'][0]['content'][0]['text'] == 'Snooze Alert'

        # Check fields are present
        all_text = json.dumps(result)
        assert 'web01' in all_text
        assert 'nagios' in all_text
        assert 'httpd' in all_text
        assert 'critical' in all_text
        assert 'HTTP service is down' in all_text

        # Check Snooze link
        assert 'snooze.example.com' in all_text
        assert 'abc123' in all_text

    def test_build_description_adf_no_url(self):
        record = {
            'host': 'web01',
            'source': 'nagios',
            'process': 'httpd',
            'severity': 'critical',
            'message': 'Test message',
        }
        result = JiraClient.build_description_adf(record, '')
        all_text = json.dumps(result)
        assert 'View in Snooze' not in all_text

    def test_build_description_adf_missing_fields(self):
        record = {}
        result = JiraClient.build_description_adf(record)
        all_text = json.dumps(result)
        assert 'Unknown' in all_text

    @patch.object(JiraClient, '_request')
    def test_create_issue(self, mock_request):
        mock_request.return_value = {'id': '10001', 'key': 'OPS-42', 'self': 'https://test.atlassian.net/rest/api/3/issue/10001'}
        result = self.client.create_issue(
            project_key='OPS',
            issue_type='Task',
            summary='Test issue',
            description_adf=JiraClient._text_to_adf('Test description'),
            priority='High',
            labels=['snooze'],
        )
        assert result['key'] == 'OPS-42'
        mock_request.assert_called_once()
        call_args = mock_request.call_args
        assert call_args[0] == ('POST', '/issue')
        payload = call_args[1]['json']
        assert payload['fields']['project']['key'] == 'OPS'
        assert payload['fields']['issuetype']['name'] == 'Task'
        assert payload['fields']['summary'] == 'Test issue'
        assert payload['fields']['priority']['name'] == 'High'
        assert payload['fields']['labels'] == ['snooze']

    @patch.object(JiraClient, '_request')
    def test_create_issue_with_extra_fields(self, mock_request):
        mock_request.return_value = {'id': '10002', 'key': 'OPS-43'}
        extra = {'components': [{'name': 'Infrastructure'}]}
        self.client.create_issue(
            project_key='OPS',
            issue_type='Bug',
            summary='Bug report',
            description_adf=JiraClient._text_to_adf('desc'),
            extra_fields=extra,
        )
        payload = mock_request.call_args[1]['json']
        assert payload['fields']['components'] == [{'name': 'Infrastructure'}]

    @patch.object(JiraClient, '_request')
    def test_create_issue_with_issue_type_id(self, mock_request):
        mock_request.return_value = {'id': '10002', 'key': 'OPS-43'}
        self.client.create_issue(
            project_key='OPS',
            issue_type='Task',
            issue_type_id='10001',
            summary='Issue by type id',
            description_adf=JiraClient._text_to_adf('desc'),
        )
        payload = mock_request.call_args[1]['json']
        assert payload['fields']['issuetype'] == {'id': '10001'}

    @patch.object(JiraClient, '_request')
    def test_create_issue_priority_string_fallback(self, mock_request):
        error_response = MagicMock()
        error_response.json.return_value = {
            'errorMessages': [],
            'errors': {'priority': 'Specify Priority (name) in string format'},
        }
        http_error = HTTPError(response=error_response)
        mock_request.side_effect = [http_error, {'id': '10003', 'key': 'OPS-44'}]

        result = self.client.create_issue(
            project_key='OPS',
            issue_type='Task',
            summary='Fallback priority format',
            description_adf=JiraClient._text_to_adf('Test description'),
            priority='High',
        )

        assert result['key'] == 'OPS-44'
        assert mock_request.call_count == 2
        first_payload = mock_request.call_args_list[0][1]['json']
        second_payload = mock_request.call_args_list[1][1]['json']
        assert first_payload['fields']['priority'] == {'name': 'High'}
        assert second_payload['fields']['priority'] == 'High'

    @patch.object(JiraClient, '_request')
    def test_add_comment(self, mock_request):
        mock_request.return_value = {'id': '100'}
        self.client.add_comment('OPS-42', 'This is a comment')
        mock_request.assert_called_once()
        call_args = mock_request.call_args
        assert call_args[0] == ('POST', '/issue/OPS-42/comment')
        body = call_args[1]['json']['body']
        assert body['type'] == 'doc'
        assert body['content'][0]['content'][0]['text'] == 'This is a comment'

    @patch.object(JiraClient, '_request')
    def test_transition_issue(self, mock_request):
        mock_request.return_value = {}
        self.client.transition_issue('OPS-42', '5', comment='Fixed')
        mock_request.assert_called_once()
        call_args = mock_request.call_args
        assert call_args[0] == ('POST', '/issue/OPS-42/transitions')
        payload = call_args[1]['json']
        assert payload['transition']['id'] == '5'
        assert 'comment' in payload['update']

    @patch.object(JiraClient, '_request')
    def test_transition_issue_no_comment(self, mock_request):
        mock_request.return_value = {}
        self.client.transition_issue('OPS-42', '5')
        payload = mock_request.call_args[1]['json']
        assert 'update' not in payload

    @patch.object(JiraClient, '_request')
    def test_get_issue(self, mock_request):
        mock_request.return_value = {'key': 'OPS-42', 'fields': {'summary': 'Test'}}
        result = self.client.get_issue('OPS-42')
        assert result['key'] == 'OPS-42'
        mock_request.assert_called_once_with('GET', '/issue/OPS-42')

    @patch.object(JiraClient, '_request')
    def test_get_transitions(self, mock_request):
        mock_request.return_value = {
            'transitions': [
                {'id': '11', 'name': 'To Do', 'to': {'name': 'To Do'}},
                {'id': '21', 'name': 'In Progress', 'to': {'name': 'In Progress'}},
            ]
        }
        result = self.client.get_transitions('OPS-42')
        assert len(result) == 2
        assert result[0]['id'] == '11'
        mock_request.assert_called_once_with('GET', '/issue/OPS-42/transitions')

    @patch.object(JiraClient, '_request')
    def test_search_issues(self, mock_request):
        mock_request.return_value = {
            'issues': [
                {'key': 'OPS-1', 'fields': {'summary': 'Test', 'customfield_10500': 'hash1'}},
                {'key': 'OPS-2', 'fields': {'summary': 'Test 2', 'customfield_10500': 'hash2'}},
            ],
        }
        result = self.client.search_issues('project = OPS', fields=['summary', 'customfield_10500'])
        assert len(result) == 2
        assert result[0]['key'] == 'OPS-1'
        mock_request.assert_called_once()
        payload = mock_request.call_args[1]['json']
        assert payload['jql'] == 'project = OPS'
        assert payload['fields'] == ['summary', 'customfield_10500']

    @patch.object(JiraClient, '_request')
    def test_search_issues_empty(self, mock_request):
        mock_request.return_value = {'issues': []}
        result = self.client.search_issues('project = OPS AND status = Done')
        assert result == []

    def test_find_user_by_email_single_result(self):
        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = [{'accountId': 'abc123', 'emailAddress': 'user@example.com'}]
        mock_resp.raise_for_status = MagicMock()
        self.client.session.get = MagicMock(return_value=mock_resp)

        result = self.client.find_user_by_email('user@example.com')
        assert result == 'abc123'
        self.client.session.get.assert_called_once()

    def test_find_user_by_email_no_result(self):
        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = []
        mock_resp.raise_for_status = MagicMock()
        self.client.session.get = MagicMock(return_value=mock_resp)

        result = self.client.find_user_by_email('nobody@example.com')
        assert result is None

    def test_find_user_by_email_multiple_exact_match(self):
        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = [
            {'accountId': 'aaa', 'emailAddress': 'alice@example.com'},
            {'accountId': 'bbb', 'emailAddress': 'bob@example.com'},
        ]
        mock_resp.raise_for_status = MagicMock()
        self.client.session.get = MagicMock(return_value=mock_resp)

        result = self.client.find_user_by_email('bob@example.com')
        assert result == 'bbb'


class TestJiraPlugin:
    """Tests for the main JiraPlugin logic."""

    def _make_plugin(self, config=None):
        from snooze_jira.main import JiraPlugin
        default_config = {
            'jira_url': 'https://test.atlassian.net',
            'jira_email': 'test@example.com',
            'jira_api_token': 'test-token',
            'project_key': 'OPS',
            'issue_type': 'Task',
            'priority': 'Medium',
            'labels': ['snooze'],
            'snooze_url': 'https://snooze.example.com',
            'summary_template': '[${severity}] ${host} - ${message}',
        }
        if config:
            default_config.update(config)
        with patch('snooze_jira.main.Snooze'):
            plugin = JiraPlugin(default_config)
        return plugin

    def test_format_summary(self):
        plugin = self._make_plugin()
        record = {
            'severity': 'critical',
            'host': 'web01',
            'message': 'HTTP service is down',
        }
        summary = plugin._format_summary(record)
        assert summary == '[critical] web01 - HTTP service is down'

    def test_format_summary_missing_fields(self):
        plugin = self._make_plugin()
        record = {}
        summary = plugin._format_summary(record)
        assert 'Unknown' in summary

    def test_format_summary_truncation(self):
        plugin = self._make_plugin()
        record = {
            'severity': 'critical',
            'host': 'web01',
            'message': 'A' * 300,
        }
        summary = plugin._format_summary(record)
        assert len(summary) <= 255

    def test_format_summary_custom_template(self):
        plugin = self._make_plugin({'summary_template': '${host}: ${severity}'})
        record = {'host': 'db01', 'severity': 'warning'}
        summary = plugin._format_summary(record)
        assert summary == 'db01: warning'

    def test_format_description_template(self):
        plugin = self._make_plugin({
            'description_template': 'Host: ${host}\nSeverity: ${severity}\nMessage: ${message}',
        })
        record = {'host': 'web01', 'severity': 'critical', 'message': 'Service down'}
        result = plugin._format_description(record)
        assert result['type'] == 'doc'
        assert result['version'] == 1
        assert len(result['content']) == 3
        assert result['content'][0]['content'][0]['text'] == 'Host: web01'
        assert result['content'][1]['content'][0]['text'] == 'Severity: critical'
        assert result['content'][2]['content'][0]['text'] == 'Message: Service down'

    def test_format_description_template_with_snooze_url(self):
        plugin = self._make_plugin({
            'description_template': 'Alert: ${host} - ${message}\nLink: ${snooze_url}/web/?#/record?tab=All&s=hash%3D${hash}',
        })
        record = {'host': 'web01', 'message': 'Down', 'hash': 'abc123'}
        result = plugin._format_description(record)
        assert 'snooze.example.com' in result['content'][1]['content'][0]['text']
        assert 'abc123' in result['content'][1]['content'][0]['text']

    @patch.object(JiraClient, 'create_issue')
    def test_description_template_used_in_process_records(self, mock_create):
        plugin = self._make_plugin({
            'description_template': 'Alert on ${host}: ${message}',
        })
        mock_create.return_value = {'id': '10030', 'key': 'OPS-80'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'desc1', 'host': 'web01', 'severity': 'critical', 'message': 'HTTP down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        desc = call_kwargs['description_adf']
        # Should be template-based (simple paragraphs), not the rich ADF with heading
        assert desc['type'] == 'doc'
        assert desc['content'][0]['content'][0]['text'] == 'Alert on web01: HTTP down'
        # Should NOT have a heading (which the default build_description_adf adds)
        assert desc['content'][0]['type'] == 'paragraph'

    @patch.object(JiraClient, 'create_issue')
    def test_default_description_when_no_template(self, mock_create):
        plugin = self._make_plugin()  # no description_template
        mock_create.return_value = {'id': '10031', 'key': 'OPS-81'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'desc2', 'host': 'web01', 'severity': 'critical', 'message': 'HTTP down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        desc = call_kwargs['description_adf']
        # Should use default rich ADF with heading
        assert desc['content'][0]['type'] == 'heading'

    def test_find_existing_issue_found(self):
        plugin = self._make_plugin()
        record = {
            'snooze_webhook_responses': [{
                'action_name': 'jira_action',
                'content': {'issue_key': 'OPS-42'},
            }],
        }
        result = plugin._find_existing_issue(record, 'jira_action')
        assert result == 'OPS-42'

    def test_find_existing_issue_not_found(self):
        plugin = self._make_plugin()
        record = {
            'snooze_webhook_responses': [{
                'action_name': 'other_action',
                'content': {'issue_key': 'OPS-42'},
            }],
        }
        result = plugin._find_existing_issue(record, 'jira_action')
        assert result is None

    def test_find_existing_issue_no_responses(self):
        plugin = self._make_plugin()
        record = {}
        result = plugin._find_existing_issue(record, 'jira_action')
        assert result is None

    def test_build_comment(self):
        plugin = self._make_plugin()
        record = {
            'timestamp': '2026-02-16T10:00:00+0000',
            'host': 'web01',
            'severity': 'critical',
            'message': 'Service down',
        }
        comment = plugin._build_comment(record, message='Please check')
        assert 'Re-escalation' in comment
        assert 'web01' in comment
        assert 'critical' in comment
        assert 'Service down' in comment
        assert 'Please check' in comment

    def test_build_comment_with_notification_from(self):
        plugin = self._make_plugin()
        record = {'host': 'web01', 'severity': 'critical', 'message': 'down'}
        notification_from = {'name': 'AlertManager', 'message': 'Auto-escalated'}
        comment = plugin._build_comment(record, notification_from=notification_from)
        assert 'AlertManager' in comment
        assert 'Auto-escalated' in comment

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_new_issue(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10001', 'key': 'OPS-42'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'abc123',
                'host': 'web01',
                'source': 'nagios',
                'process': 'httpd',
                'severity': 'critical',
                'message': 'HTTP down',
            },
        }]

        result = plugin.process_records(req, medias)
        assert 'abc123' in result
        assert result['abc123']['issue_key'] == 'OPS-42'
        mock_create.assert_called_once()

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_sets_alert_hash_custom_field(self, mock_create):
        plugin = self._make_plugin({'alert_hash_custom_field': 'customfield_10500'})
        mock_create.return_value = {'id': '10001', 'key': 'OPS-42'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'uid': 'uid-123',
                'hash': 'abc123',
                'host': 'web01',
                'source': 'nagios',
                'process': 'httpd',
                'severity': 'critical',
                'message': 'HTTP down',
            },
        }]

        plugin.process_records(req, medias)

        call_kwargs = mock_create.call_args[1]
        field_value = call_kwargs['extra_fields']['customfield_10500']
        # Should be a Snooze URL containing the hash
        assert 'snooze.example.com' in field_value
        assert 'hash%3Dabc123' in field_value

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_no_custom_field_when_not_configured(self, mock_create):
        plugin = self._make_plugin()  # no alert_hash_custom_field
        mock_create.return_value = {'id': '10001', 'key': 'OPS-42'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'abc123',
                'host': 'web01',
                'severity': 'critical',
                'message': 'HTTP down',
            },
        }]

        plugin.process_records(req, medias)

        call_kwargs = mock_create.call_args[1]
        # No custom field key set, so extra_fields should not contain any customfield
        for key in call_kwargs['extra_fields']:
            assert not key.startswith('customfield_')

    @patch.object(JiraClient, 'add_comment')
    def test_process_records_existing_issue(self, mock_comment):
        plugin = self._make_plugin()
        mock_comment.return_value = {'id': '200'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'abc123',
                'host': 'web01',
                'severity': 'critical',
                'message': 'HTTP down again',
                'snooze_webhook_responses': [{
                    'action_name': 'jira_action',
                    'content': {'issue_key': 'OPS-42'},
                }],
            },
        }]

        result = plugin.process_records(req, medias)
        assert result['abc123']['issue_key'] == 'OPS-42'
        mock_comment.assert_called_once()

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_custom_project(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10002', 'key': 'INFRA-1'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'project_key': 'INFRA',
            'issue_type': 'Bug',
            'priority': 'High',
            'alert': {
                'hash': 'def456',
                'host': 'db01',
                'severity': 'warning',
                'message': 'Disk space low',
            },
        }]

        result = plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['project_key'] == 'INFRA'
        assert call_kwargs['issue_type'] == 'Bug'
        assert call_kwargs['priority'] == 'High'

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_issue_type_id_from_payload(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10002', 'key': 'INFRA-1'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'project_key': 'INFRA',
            'issue_type': 'Bug',
            'issue_type_id': '10002',
            'priority': 'High',
            'alert': {
                'hash': 'itype1',
                'host': 'db01',
                'severity': 'warning',
                'message': 'Disk space low',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['issue_type'] == 'Bug'
        assert call_kwargs['issue_type_id'] == '10002'

    @patch.object(JiraClient, 'create_issue')
    def test_process_records_issue_type_id_from_config(self, mock_create):
        plugin = self._make_plugin({'issue_type_id': '10005'})
        mock_create.return_value = {'id': '10003', 'key': 'OPS-2'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'itype2',
                'host': 'db01',
                'severity': 'warning',
                'message': 'Disk space low',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['issue_type_id'] == '10005'

    @patch.object(JiraClient, 'create_issue')
    def test_payload_issue_type_beats_config_issue_type_id(self, mock_create):
        plugin = self._make_plugin({'issue_type_id': '10005', 'issue_type': 'Task'})
        mock_create.return_value = {'id': '10004', 'key': 'OPS-3'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'issue_type': 'Epic',
            'alert': {
                'hash': 'itype3',
                'host': 'db01',
                'severity': 'warning',
                'message': 'Disk space low',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['issue_type'] == 'Epic'
        assert call_kwargs['issue_type_id'] == ''

    @patch.object(JiraClient, 'create_issue')
    def test_priority_mapping_critical(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10003', 'key': 'OPS-50'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'prio1',
                'host': 'web01',
                'severity': 'critical',
                'message': 'Down',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['priority'] == 'High'

    @patch.object(JiraClient, 'create_issue')
    def test_priority_mapping_info(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10004', 'key': 'OPS-51'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'prio2',
                'host': 'web01',
                'severity': 'info',
                'message': 'Informational',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['priority'] == 'Lowest'

    @patch.object(JiraClient, 'create_issue')
    def test_priority_mapping_unknown_severity_uses_default(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10005', 'key': 'OPS-52'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'prio3',
                'host': 'web01',
                'severity': 'unknown_level',
                'message': 'Something',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['priority'] == 'Medium'  # default_priority

    @patch.object(JiraClient, 'create_issue')
    def test_priority_payload_override_beats_mapping(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10006', 'key': 'OPS-53'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'priority': 'Low',
            'alert': {
                'hash': 'prio4',
                'host': 'web01',
                'severity': 'critical',
                'message': 'Override',
            },
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['priority'] == 'Low'

    @patch.object(JiraClient, 'transition_issue')
    @patch.object(JiraClient, 'get_transitions')
    @patch.object(JiraClient, 'get_issue')
    @patch.object(JiraClient, 'add_comment')
    def test_reopen_closed_issue(self, mock_comment, mock_get_issue, mock_get_trans, mock_transition):
        plugin = self._make_plugin({'reopen_closed': True, 'reopen_status_name': 'To Do'})
        mock_comment.return_value = {'id': '200'}
        mock_get_issue.return_value = {
            'fields': {
                'status': {
                    'name': 'Done',
                    'statusCategory': {'key': 'done'},
                },
            },
        }
        mock_get_trans.return_value = [
            {'id': '11', 'name': 'Reopen', 'to': {'name': 'To Do', 'statusCategory': {'key': 'new'}}},
        ]
        mock_transition.return_value = {}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'reopen1',
                'host': 'web01',
                'severity': 'critical',
                'message': 'Back again',
                'snooze_webhook_responses': [{
                    'action_name': 'jira_action',
                    'content': {'issue_key': 'OPS-42'},
                }],
            },
        }]

        result = plugin.process_records(req, medias)
        mock_comment.assert_called_once()
        mock_get_issue.assert_called_once_with('OPS-42')
        mock_transition.assert_called_once_with('OPS-42', '11', comment='Reopened by Snooze due to re-escalation')

    @patch.object(JiraClient, 'get_issue')
    @patch.object(JiraClient, 'add_comment')
    def test_no_reopen_when_disabled(self, mock_comment, mock_get_issue):
        plugin = self._make_plugin({'reopen_closed': False})
        mock_comment.return_value = {'id': '200'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'reopen2',
                'host': 'web01',
                'severity': 'critical',
                'message': 'Still down',
                'snooze_webhook_responses': [{
                    'action_name': 'jira_action',
                    'content': {'issue_key': 'OPS-42'},
                }],
            },
        }]

        plugin.process_records(req, medias)
        mock_comment.assert_called_once()
        mock_get_issue.assert_not_called()  # Should not even check issue status

    @patch.object(JiraClient, 'transition_issue')
    @patch.object(JiraClient, 'get_transitions')
    @patch.object(JiraClient, 'get_issue')
    @patch.object(JiraClient, 'add_comment')
    def test_reopen_fallback_transition(self, mock_comment, mock_get_issue, mock_get_trans, mock_transition):
        """When the configured reopen_status_name doesn't match, fall back to any non-done transition."""
        plugin = self._make_plugin({'reopen_closed': True, 'reopen_status_name': 'Open'})
        mock_comment.return_value = {'id': '200'}
        mock_get_issue.return_value = {
            'fields': {
                'status': {
                    'name': 'Done',
                    'statusCategory': {'key': 'done'},
                },
            },
        }
        mock_get_trans.return_value = [
            {'id': '21', 'name': 'In Progress', 'to': {'name': 'In Progress', 'statusCategory': {'key': 'indeterminate'}}},
        ]
        mock_transition.return_value = {}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'alert': {
                'hash': 'reopen3',
                'host': 'web01',
                'severity': 'warning',
                'message': 'Back',
                'snooze_webhook_responses': [{
                    'action_name': 'jira_action',
                    'content': {'issue_key': 'OPS-99'},
                }],
            },
        }]

        plugin.process_records(req, medias)
        # Should fall back to 'In Progress' transition
        mock_transition.assert_called_once_with('OPS-99', '21', comment='Reopened by Snooze due to re-escalation')

    @patch.object(JiraClient, 'create_issue')
    def test_assignee_from_config(self, mock_create):
        plugin = self._make_plugin({'assignee': '5b109f2e9729b51b54dc274d'})
        mock_create.return_value = {'id': '10010', 'key': 'OPS-60'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'assign1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['assignee'] == {'id': '5b109f2e9729b51b54dc274d'}

    @patch.object(JiraClient, 'find_user_by_email', return_value='resolved-account-id-1')
    @patch.object(JiraClient, 'create_issue')
    def test_assignee_email_from_config(self, mock_create, mock_find):
        plugin = self._make_plugin({'assignee': 'john@example.com'})
        mock_create.return_value = {'id': '10010', 'key': 'OPS-60'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'assign_email1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        mock_find.assert_called_once_with('john@example.com')
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['assignee'] == {'id': 'resolved-account-id-1'}

    @patch.object(JiraClient, 'create_issue')
    def test_reporter_from_config(self, mock_create):
        plugin = self._make_plugin({'reporter': '5b10a2844c20165700ede21g'})
        mock_create.return_value = {'id': '10011', 'key': 'OPS-61'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'report1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['reporter'] == {'id': '5b10a2844c20165700ede21g'}

    @patch.object(JiraClient, 'find_user_by_email', return_value='resolved-account-id-2')
    @patch.object(JiraClient, 'create_issue')
    def test_reporter_email_from_config(self, mock_create, mock_find):
        plugin = self._make_plugin({'reporter': 'jane@example.com'})
        mock_create.return_value = {'id': '10011', 'key': 'OPS-61'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'report_email1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        mock_find.assert_called_once_with('jane@example.com')
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['reporter'] == {'id': 'resolved-account-id-2'}

    @patch.object(JiraClient, 'create_issue')
    def test_custom_fields_from_config(self, mock_create):
        custom = {
            'customfield_10100': {'value': 'Infrastructure'},
            'customfield_10718': [{'id': '11688', 'value': 'DevOps'}],
        }
        plugin = self._make_plugin({'custom_fields': custom})
        mock_create.return_value = {'id': '10012', 'key': 'OPS-62'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'cf1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['customfield_10100'] == {'value': 'Infrastructure'}
        assert call_kwargs['extra_fields']['customfield_10718'] == [{'id': '11688', 'value': 'DevOps'}]

    @patch.object(JiraClient, 'create_issue')
    def test_custom_fields_payload_override(self, mock_create):
        config_custom = {'customfield_10100': {'value': 'Infrastructure'}}
        payload_custom = {'customfield_10100': {'value': 'Networking'}, 'customfield_10200': {'value': 'Team A'}}
        plugin = self._make_plugin({'custom_fields': config_custom})
        mock_create.return_value = {'id': '10013', 'key': 'OPS-63'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'custom_fields': payload_custom,
            'alert': {'hash': 'cf2', 'host': 'web01', 'severity': 'critical', 'message': 'Down'},
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        # Payload should override config for same field
        assert call_kwargs['extra_fields']['customfield_10100'] == {'value': 'Networking'}
        # Payload-only field should be present
        assert call_kwargs['extra_fields']['customfield_10200'] == {'value': 'Team A'}

    @patch.object(JiraClient, 'create_issue')
    def test_assignee_payload_override(self, mock_create):
        plugin = self._make_plugin({'assignee': 'config_user'})
        mock_create.return_value = {'id': '10013', 'key': 'OPS-63'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'assignee': 'payload_user',
            'alert': {'hash': 'assign2', 'host': 'web01', 'severity': 'critical', 'message': 'Down'},
        }]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['assignee'] == {'id': 'payload_user'}

    @patch.object(JiraClient, 'find_user_by_email', return_value='resolved-override-id')
    @patch.object(JiraClient, 'create_issue')
    def test_assignee_payload_override_email(self, mock_create, mock_find):
        plugin = self._make_plugin({'assignee': 'config_id'})
        mock_create.return_value = {'id': '10013', 'key': 'OPS-63'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'assignee': 'override@example.com',
            'alert': {'hash': 'assign3', 'host': 'web01', 'severity': 'critical', 'message': 'Down'},
        }]

        plugin.process_records(req, medias)
        mock_find.assert_called_once_with('override@example.com')
        call_kwargs = mock_create.call_args[1]
        assert call_kwargs['extra_fields']['assignee'] == {'id': 'resolved-override-id'}

    @patch.object(JiraClient, 'create_issue')
    def test_no_assignee_when_empty(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10014', 'key': 'OPS-64'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'noassign', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        assert 'assignee' not in call_kwargs['extra_fields']

    @patch.object(JiraClient, 'create_issue')
    def test_no_custom_fields_when_empty(self, mock_create):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10015', 'key': 'OPS-65'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'nocf', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        call_kwargs = mock_create.call_args[1]
        # No custom fields, no assignee, no reporter => extra_fields should be empty
        assert call_kwargs['extra_fields'] == {}

    @patch.object(JiraClient, 'find_user_by_email', return_value=None)
    @patch.object(JiraClient, 'create_issue')
    def test_assignee_email_lookup_failure_skips_field(self, mock_create, mock_find):
        plugin = self._make_plugin({'assignee': 'unknown@example.com'})
        mock_create.return_value = {'id': '10016', 'key': 'OPS-66'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'fail_email', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        mock_find.assert_called_once_with('unknown@example.com')
        call_kwargs = mock_create.call_args[1]
        # Assignee should be skipped when email lookup fails
        assert 'assignee' not in call_kwargs['extra_fields']

    @patch.object(JiraClient, 'find_user_by_email', return_value='cached-id')
    @patch.object(JiraClient, 'create_issue')
    def test_email_lookup_is_cached(self, mock_create, mock_find):
        plugin = self._make_plugin({'assignee': 'cached@example.com'})
        mock_create.return_value = {'id': '10017', 'key': 'OPS-67'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [
            {'alert': {'hash': 'cache1', 'host': 'web01', 'severity': 'critical', 'message': 'First'}},
            {'alert': {'hash': 'cache2', 'host': 'web02', 'severity': 'warning', 'message': 'Second'}},
        ]

        plugin.process_records(req, medias)
        # Should only call find_user_by_email once due to caching
        mock_find.assert_called_once_with('cached@example.com')
        assert mock_create.call_count == 2

    @patch.object(JiraClient, 'transition_issue')
    @patch.object(JiraClient, 'get_transitions')
    @patch.object(JiraClient, 'create_issue')
    def test_initial_status_transitions_after_create(self, mock_create, mock_get_trans, mock_transition):
        plugin = self._make_plugin({'initial_status': 'In Progress'})
        mock_create.return_value = {'id': '10020', 'key': 'OPS-70'}
        mock_get_trans.return_value = [
            {'id': '21', 'name': 'Start Progress', 'to': {'name': 'In Progress', 'statusCategory': {'key': 'indeterminate'}}},
        ]
        mock_transition.return_value = {}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'init1', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        mock_create.assert_called_once()
        mock_get_trans.assert_called_once_with('OPS-70')
        mock_transition.assert_called_once_with('OPS-70', '21', comment=None)

    @patch.object(JiraClient, 'transition_issue')
    @patch.object(JiraClient, 'get_transitions')
    @patch.object(JiraClient, 'create_issue')
    def test_initial_status_payload_override(self, mock_create, mock_get_trans, mock_transition):
        plugin = self._make_plugin({'initial_status': 'In Progress'})
        mock_create.return_value = {'id': '10021', 'key': 'OPS-71'}
        mock_get_trans.return_value = [
            {'id': '31', 'name': 'Review', 'to': {'name': 'In Review', 'statusCategory': {'key': 'indeterminate'}}},
        ]
        mock_transition.return_value = {}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{
            'initial_status': 'In Review',
            'alert': {'hash': 'init2', 'host': 'web01', 'severity': 'critical', 'message': 'Down'},
        }]

        plugin.process_records(req, medias)
        mock_get_trans.assert_called_once_with('OPS-71')
        mock_transition.assert_called_once_with('OPS-71', '31', comment=None)

    @patch.object(JiraClient, 'get_transitions')
    @patch.object(JiraClient, 'create_issue')
    def test_no_initial_status_when_not_configured(self, mock_create, mock_get_trans):
        plugin = self._make_plugin()
        mock_create.return_value = {'id': '10022', 'key': 'OPS-72'}

        req = MagicMock()
        req.params = {'snooze_action_name': 'jira_action'}

        medias = [{'alert': {'hash': 'init3', 'host': 'web01', 'severity': 'critical', 'message': 'Down'}}]

        plugin.process_records(req, medias)
        mock_create.assert_called_once()
        mock_get_trans.assert_not_called()


class TestJiraPoller:
    """Tests for the background JIRA polling logic."""

    def _make_plugin(self, config=None):
        from snooze_jira.main import JiraPlugin
        default_config = {
            'jira_url': 'https://test.atlassian.net',
            'jira_email': 'test@example.com',
            'jira_api_token': 'test-token',
            'project_key': 'OPS',
            'snooze_url': 'https://snooze.example.com',
            'alert_hash_custom_field': 'customfield_10500',
            'poll_enabled': True,
            'poll_interval': 60,
        }
        if config:
            default_config.update(config)
        with patch('snooze_jira.main.Snooze'):
            plugin = JiraPlugin(default_config)
        # Initialize tracked issues dict (normally done by _start_poller)
        plugin._tracked_issues = {}
        return plugin

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_populates_tracked_issues(self, mock_search):
        plugin = self._make_plugin()
        mock_search.return_value = [
            {'key': 'OPS-1', 'fields': {'customfield_10500': 'https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dhash_a', 'status': {'name': 'Open'}}},
            {'key': 'OPS-2', 'fields': {'customfield_10500': 'https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dhash_b', 'status': {'name': 'In Progress'}}},
        ]

        plugin._poll_cycle()

        assert plugin._tracked_issues == {'OPS-1': 'hash_a', 'OPS-2': 'hash_b'}
        mock_search.assert_called_once()

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_detects_closed_issues(self, mock_search):
        plugin = self._make_plugin()
        # Simulate previous cycle had two open issues
        plugin._tracked_issues = {'OPS-1': 'hash_a', 'OPS-2': 'hash_b'}

        # Now only OPS-1 is open (OPS-2 was closed in JIRA)
        mock_search.return_value = [
            {'key': 'OPS-1', 'fields': {'customfield_10500': 'https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dhash_a', 'status': {'name': 'Open'}}},
        ]

        # Mock the Snooze client to verify close is called
        plugin.client.record = MagicMock(return_value=[{'uid': 'uid-b'}])
        plugin.client.comment_batch = MagicMock()

        plugin._poll_cycle()

        # OPS-2 should have been detected as closed
        plugin.client.record.assert_called_once_with(['=', 'hash', 'hash_b'])
        plugin.client.comment_batch.assert_called_once_with([{
            'type': 'close',
            'record_uid': 'uid-b',
            'name': 'jira',
            'method': 'jira',
            'message': 'Closed: JIRA ticket OPS-2 resolved',
        }])

        # Tracked issues should only contain OPS-1 now
        assert plugin._tracked_issues == {'OPS-1': 'hash_a'}

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_no_close_when_all_still_open(self, mock_search):
        plugin = self._make_plugin()
        plugin._tracked_issues = {'OPS-1': 'hash_a'}

        mock_search.return_value = [
            {'key': 'OPS-1', 'fields': {'customfield_10500': 'https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dhash_a', 'status': {'name': 'Open'}}},
        ]

        plugin.client.record = MagicMock()
        plugin.client.comment_batch = MagicMock()

        plugin._poll_cycle()

        plugin.client.record.assert_not_called()
        plugin.client.comment_batch.assert_not_called()
        assert plugin._tracked_issues == {'OPS-1': 'hash_a'}

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_no_snooze_record_found(self, mock_search):
        plugin = self._make_plugin()
        plugin._tracked_issues = {'OPS-1': 'hash_a'}

        # OPS-1 closed
        mock_search.return_value = []

        plugin.client.record = MagicMock(return_value=[])
        plugin.client.comment_batch = MagicMock()

        plugin._poll_cycle()

        # Record lookup was attempted but found nothing
        plugin.client.record.assert_called_once_with(['=', 'hash', 'hash_a'])
        # comment_batch should NOT be called since no record was found
        plugin.client.comment_batch.assert_not_called()

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_uses_custom_jql(self, mock_search):
        plugin = self._make_plugin({'poll_jql': 'project = INFRA AND status != Done'})
        mock_search.return_value = []

        plugin._poll_cycle()

        call_kwargs = mock_search.call_args
        assert call_kwargs[0][0] == 'project = INFRA AND status != Done'

    @patch.object(JiraClient, 'search_issues')
    def test_poll_cycle_default_jql_uses_cf_syntax(self, mock_search):
        plugin = self._make_plugin()
        mock_search.return_value = []

        plugin._poll_cycle()

        call_kwargs = mock_search.call_args
        assert call_kwargs[0][0] == 'cf[10500] is not EMPTY AND statusCategory != Done'

    def test_start_poller_disabled(self):
        plugin = self._make_plugin({'poll_enabled': False})
        plugin._start_poller()
        assert not hasattr(plugin, '_poll_thread')

    def test_start_poller_no_custom_field(self):
        plugin = self._make_plugin({'poll_enabled': True, 'alert_hash_custom_field': ''})
        plugin._start_poller()
        assert not hasattr(plugin, '_poll_thread')

    def test_extract_hash_from_url(self):
        from snooze_jira.main import JiraPlugin
        url = 'https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dabc123'
        assert JiraPlugin._extract_hash_from_field(url) == 'abc123'

    def test_extract_hash_from_plain_string(self):
        from snooze_jira.main import JiraPlugin
        assert JiraPlugin._extract_hash_from_field('abc123') == 'abc123'

    def test_extract_hash_from_empty(self):
        from snooze_jira.main import JiraPlugin
        assert JiraPlugin._extract_hash_from_field('') == ''
        assert JiraPlugin._extract_hash_from_field(None) == ''
