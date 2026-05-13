# Microsoft Teams Bot plugin

This plugin is used to display SnoozeWeb alerts in Teams using a chatbot. Users can also partially manage these alerts directly from the chat.

# Installation

```console
$ sudo /opt/snooze/bin/pip install git+https://github.com/snoozeweb/snooze_plugins.git#subdirectory=output/teams
$ sudo tee <<SERVICE /etc/systemd/system/snooze-teams.service
[Unit]
Description=Snooze teams output plugin
After=network.service

[Service]
User=snooze
ExecStart=/opt/snooze/bin/snooze-teams
Restart=always

[Install]
WantedBy=multi-user.target
SERVICE

$ sudo systemctl daemon-reload
$ sudo systemctl enable snooze-teams
$ sudo systemctl start snooze-teams
```

# Prerequisites

* [Snooze Client](https://github.com/snoozeweb/snooze_client): For Snooze Teams daemon to use Snooze Server API
* [Azure App](https://learn.microsoft.com/en-us/graph/auth-register-app-v2): Microsoft Azure App to post messages in Teams 
* Snooze Action (webhook): Communication between Snooze Server and Snooze Teams daemon. See below

Recommended Microsoft Graph delegated permissions for polling + replies:
* `ChannelMessage.Send`
* `ChannelMessage.Read.All`
* `Chat.ReadBasic`
* `Team.ReadBasic.All`
* `Channel.ReadBasic.All`

Optional (only if you also poll chat resources):
* `Chat.Read`

## Create Action

In SnoozeWeb, go to the _Actions_ tab then click on **New**

Configuration hints:
* In _Action_, select `Call a webhook`
* In _URL_, put the alert enpoint of the plugin's daemon (if the daemon runs on the same server as Snooze-server: http://localhost:5202/alert)
* In _Payload_, put `{"channels": ["********"], "alert": {{ __self__  | tojson() }} }`
  * Replace `********` with Teams Channel ID (ex: `teams/500739d4-3478-4304-bf16-23a4ae0f09ff/channels/19:jKFUn31rBB3gt6WCW2ZA7N6y-n2GWd3kiw`)
  * To post as **replies in a thread** instead of new top-level messages, append `/messages/{parent_message_id}` to the channel ID (ex: `teams/.../channels/19:.../messages/1616990032035`)
* Check `Inject Response`
* Check `Batch` if you want multiple alerts to be grouped in the same thread

* The plugin **auto-detects** the channel layout type (Posts or Threads) via the Microsoft Graph API. No extra configuration is needed:
  * **Posts** layout channels: Messages are sent as adaptive cards (current behavior)
  * **Threads** (chat) layout channels: Messages are sent as flat HTML. Re-escalations are posted as replies to the original message

* The plugin can now process user messages **without public inbound webhook access** by polling Graph messages for known/configured channels:
  * Channels used by Snooze alert actions are learned automatically.
  * You can also preconfigure resources with `poll_resources`.

## Create Notification

In SnoozeWeb, go to the _Notifications_ tab then click on **New** or **Edit** an existing notification
In _Actions_, select the one you just created

# Configuration

This plugin's configuration is in the following YAML file: `/etc/snooze/teamsbot.yaml` (`/etc/snooze` can be overridden by the environment variable `SNOOZE_MATTERMOSTBOT_PATH`)

* `teams_url` (String, defaults to `http://localhost`): Teams URL
* `teams_port` (Integer, defaults to `8065`): Teams port
* `app_id` (String): Teams App ID
* `tenant_id` (String, **required**): Teams tenant ID
* `client_id` (String, **required**): Teams client ID
* `client_secret` (String, **required**): Teams client secret
* `ssl_verify` (Boolean defaults to `false`): Use SSL verification between the daemon and Teams
* `listening_address` (String, defaults to `'0.0.0.0'`): Address to listen to
* `listening_port` (Integer, defaults to `5202`): Port to listen to. If lower than 1024, need to run the process as root
* `snooze_url` (String, defaults to `'http://localhost:5200'`): URL to Snooze Web UI
* `date_format` (String, defaults to `'%a, %b %d, %Y at %I:%M %p'`): Date format
* `message_limit` (Integer, defaults to `10`): Maximum number of alerts to explicitly show in the same thread
* `snooze_limit` (Integer, defaults to `message_limit` value): Maximum number of alerts that can be snoozed at the same time without using an explicit condition
* `bot_name` (String, defaults to `'Bot'`): Teams Bot name
* `debug` (Boolean, defaults to `false`): Show debug logs
* `poll_interval_seconds` (Integer, defaults to `10`): Delay between each polling cycle
* `poll_lookback_seconds` (Integer, defaults to `0`): Initial lookback window for startup checkpoint (with default `0`, only messages newer than startup are processed)
* `poll_resources` (List of strings, optional): Explicit resources to poll. Accepted forms:
  * Channel ID-like form (ex: `teams/{team-id}/channels/{channel-id}`)
  * Graph relative form (ex: `/teams/{team-id}/channels/{channel-id}/messages`)
  * Full Graph URL
  * Teams channel URL copied from the Teams UI (ex: `https://teams.microsoft.com/l/channel/...?...groupId=...`)
