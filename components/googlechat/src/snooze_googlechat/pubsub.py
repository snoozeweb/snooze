"""The class managing the pubsub connectivity"""

import json
import logging
import time
import threading

# from google.oauth2.service_account import Credentials
from googleapiclient.discovery import build
from google.auth.jwt import Credentials
from google.cloud.pubsub_v1 import SubscriberClient
from google.cloud.pubsub_v1.subscriber.message import Message
from pydantic import ValidationError
from google.oauth2 import service_account

from snooze_client import Snooze
from snooze_googlechat.config import Config
from snooze_googlechat.types import Event

log = logging.getLogger("snooze.googlechat")

PUB_AUDIENCE = "https://pubsub.googleapis.com/google.pubsub.v1.Publisher"
SUB_AUDIENCE = "https://pubsub.googleapis.com/google.pubsub.v1.Subscriber"
CHAT_SCOPE = "https://www.googleapis.com/auth/chat.bot"
REACTION_SCOPE = "https://www.googleapis.com/auth/chat.messages.reactions.create"

GOOGLECHAT_RETRIES = 3

HELP_MESSAGE = """
List of availabile commands:
* `/ack`: _Acnowledge an alert_
* `/esc` [TEAM]: _Escalate the alert to a given team_
* `/close`: _Close the alert_
* `/snooze` [HOURS]: _Snooze an alert for a number of hours_
* `/help`: _Display this help message_  
"""


class PubSub(threading.Thread):
    def __init__(self, config: Config):
        super(PubSub, self).__init__()

        credentials = Credentials.from_service_account_file(
            config.service_account_path, audience=SUB_AUDIENCE
        )

        sa_credentials = service_account.Credentials.from_service_account_file(
            config.service_account_path
        )
        self.chat = build(
            "chat",
            "v1",
            credentials=sa_credentials.with_scopes([CHAT_SCOPE, REACTION_SCOPE]),
        )

        self.subscriber = SubscriberClient(
            credentials=credentials, client_options={"scopes": [SUB_AUDIENCE]}
        )

        subscription_path = self.subscriber.subscription_path(
            config.project_id, config.subscription_name
        )

        self.snooze = Snooze()

        # self.subscriber.create_subscription(name=subscription_path, topic="snooze")
        self.future = self.subscriber.subscribe(
            subscription_path, callback=self.callback
        )

    def callback(self, message: Message):
        data = json.loads(message.data)

        try:
            event = Event(**data)
        except ValidationError as err:
            log.error(f"failed to validate message: {err}")
            return

        try:
            command = ""
            if event.type == "MESSAGE":
                command = get_message_command(event)
            elif event.type == "CARD_CLICKED":
                command = get_card_command(event)
            else:
                raise Exception(f"Unknown event type: {event.type}")

            if command in ["SNOOZE_ACTION", "/snooze"]:
                self.process_snooze(event)
            elif command in ["SNOOZE_DIALOG"]:
                ...
            elif command in ["ACK", "/ack"]:
                self.process_ack(event)
            elif command in ["ESCALATE", "/esc"]:
                self.process_escalate(event)
            elif command in ["CLOSE", "/close"]:
                self.process_close(event)
            elif command in ["REOPEN", "reopen"]:
                self.process_reopen(event)
            elif command in ["/help"]:
                self.process_help(event)
            else:
                raise Exception(f"Unknown command: '{command}'")
        except Exception as err:
            log.error(f"failed to process message: {err}")
        message.ack()

    def run(self):
        log.debug("Wait for messages...")
        with self.subscriber:
            try:
                log.debug("got subscriber")
                self.future.result()
            except TimeoutError:
                log.error("timeout error")
                self.future.cancel()
                self.future.result()

    def kill(self):
        self.future.cancel()
        self.future.result()
        self.join()

    def send_reaction(self, reaction: str, space: str, msg: str):
        for _ in range(GOOGLECHAT_RETRIES):
            try:
                resp = (
                    self.chat.spaces()
                    .messages()
                    .reactions()
                    .create(
                        parent=space,
                        messageReplyOption="REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD",
                        body=msg,
                    )
                    .create(
                        parent=msg,
                        body={
                            "emoji": {"unicode": reaction},
                        },
                    )
                    .execute()
                )

                return resp
            except Exception as err:
                log.exception(err)
                time.sleep(1)
                continue
        log.error("failed 3 times to send message to googlechat")

    def send_reply(self, text: str, space: str, thread: str):
        for _ in range(GOOGLECHAT_RETRIES):
            try:
                resp = (
                    self.chat.spaces()
                    .messages()
                    .create(
                        parent=space,
                        messageReplyOption="REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD",
                        body={"thread": {"name": thread}, "text": text},
                    )
                    .execute()
                )

                return resp
            except Exception as err:
                log.exception(err)
                time.sleep(1)
                continue
        log.error("failed 3 times to send message to googlechat")

    def process_snooze(self, _event: Event):
        raise NotImplementedError

    def process_ack(self, event: Event):
        if event.message is None:
            raise Exception("Message is empty")
        thread_name = event.message.thread.name
        log.info(f"COMMAND=ack, thread={thread_name}")

        try:
            record = find_record_by_thread(self.snooze, thread_name)
            action_name = get_record_action_name(record, thread_name)
            username = f"{event.user.displayName} via {action_name}"
            self.snooze.comment(
                "ack", username, "googlechat", record["uid"], "acked by googlechat"
            )
            message = f"✅ Alert acknowledged successfully by {username}"
        except Exception as err:
            message = f"❌ Failed to acknowledge alert: {err}"
        self.send_reply(message, event.message.space.name, event.message.thread.name)

    def process_escalate(self, _event: Event):
        raise NotImplementedError

    def process_close(self, _event: Event):
        raise NotImplementedError

    def process_reopen(self, _event: Event):
        raise NotImplementedError

    def process_help(self, event: Event):
        if event.message is None:
            raise Exception("Message is empty")
        if event.message.thread is None:
            raise Exception("Thread is empty")
        log.debug(f"COMMAND=help, thread={event.message.thread.name}")
        self.send_reply(
            HELP_MESSAGE,
            space=event.message.space.name,
            thread=event.message.thread.name,
        )


def find_record_by_thread(snooze: Snooze, thread_name: str) -> dict:
    log.debug(f"Trying to find record with thread {thread_name}")
    query = [
        "OR",
        ["IN", ["IN", thread_name, "content.threads"], "snooze_webhook_responses"],
        [
            "IN",
            ["IN", thread_name, "content.multithreads"],
            "snooze_webhook_responses",
        ],
    ]
    records = snooze.record(query)
    if len(records) == 0:
        raise Exception(f"record for thread {thread_name} not found")

    return records[0]


def get_record_action_name(record, thread_name: str) -> str:
    for action_result in record.get("snooze_webhook_responses", []):
        threads = action_result.get("content", {}).get("threads", [])
        threads += action_result.get("content", {}).get("multithreads", [])
        if thread_name in threads:
            return action_result.get("action_name")
    # Default
    return "GoogleChatBot"


def get_message_command(event: Event) -> str:
    """Retrieve the command from a message"""
    if event.message is None:
        raise Exception("Message is empty")
    if event.message.slashCommand is not None:
        return event.message.text.lstrip()
    elif event.message.argumentText is not None:
        return event.message.argumentText.lstrip()
    else:
        return event.message.text.lstrip()


def get_card_command(event: Event) -> str:
    """Retrieve the command from a card click"""
    if event.common is None:
        raise Exception("cannot find attribute 'common'")
    if event.common.invokedFunction is None:
        raise Exception("cannot find invokedFunction")
    return event.common.invokedFunction
