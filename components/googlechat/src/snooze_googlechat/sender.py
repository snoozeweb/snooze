import re
import logging
import socket
import time
from typing import List, Optional, Tuple


from apiclient.discovery import build
from google.oauth2.service_account import Credentials

# from google.auth.jwt import Credentials
from snooze_client import Snooze

from snooze_googlechat.card import new_card_v2
from snooze_googlechat.types import (
    SnoozeAlertRequest,
    SnoozeWebhookResponseContent,
    AlertWithURL,
)
from snooze_googlechat.config import Config

log = logging.getLogger("snooze.googlechat")
logging.getLogger("google").setLevel(logging.DEBUG)
logging.getLogger("googleapiclient").setLevel(logging.DEBUG)
socket.setdefaulttimeout(10)

date_regex = re.compile(
    r"[0-9]{1,4}-[0-9]{1,2}-[0-9]{1,2}T[0-9]{1,2}:[0-9]{1,2}:[0-9]{1,2}[\+\d]*"
)
duration_regex = re.compile(
    r"((\d+) *(mins|min|m|hours|hour|h|weeks|week|w|days|day|d|months|month|years|year|y)|forever){0,1} *(.*)",
    re.IGNORECASE,
)

GOOGLECHAT_AUDIENCE = "https://www.googleapis.com/auth/chat.bot"
CHAT_SCOPE = "https://www.googleapis.com/auth/chat.bot"


class Sender:
    def __init__(self, config: Config):
        credentials = Credentials.from_service_account_file(config.service_account_path)
        # self.chat = chat_v1.ChatServiceClient(
        #     credentials=self.credentials,
        #     client_options={"scopes": [GOOGLECHAT_AUDIENCE]},
        # )
        self.chat = build(
            "chat", "v1", credentials=credentials.with_scopes([CHAT_SCOPE])
        )
        self.snooze = Snooze()
        self.credentials = credentials
        self.config = config

    def send_new_message(self, req: AlertWithURL, space: str) -> dict:
        """Send a new alert to googlechat"""
        card = new_card_v2(req)
        msg = {"cards_v2": [card]}
        log.debug(f"Posting message on {space}")
        err = None
        for _ in range(3):
            try:
                resp = (
                    self.chat.spaces()
                    .messages()
                    .create(
                        parent=space,
                        messageReplyOption="MESSAGE_REPLY_OPTION_UNSPECIFIED",
                        body=msg,
                    )
                    .execute()
                )
                log.debug(f"Received response: {resp}")
                return resp
            except Exception as e:
                err = e
                log.exception(e)
                time.sleep(1)
                continue
        raise Exception(f"failed to send to googlechat (retried 3 times): {err}")

    def send_reply(self, req: AlertWithURL, space: str, thread: str) -> dict:
        """Send a reply to an existing thread"""
        card = new_card_v2(req)
        msg = {"cards_v2": [card], "thread": {"name": thread}}
        log.debug(f"Posting reply on {space}/{thread}")
        err = None
        for _ in range(3):
            try:
                resp = (
                    self.chat.spaces()
                    .messages()
                    .create(
                        parent=space,
                        messageReplyOption="REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD",
                        body=msg,
                    )
                    .execute()
                )
                log.debug("Received response: %s", str(resp.to.json()))
                return resp
            except Exception as e:
                err = e
                log.exception(e)
                time.sleep(1)
                continue
        raise Exception(f"failed to send to googlechat (retried 3 times): {err}")

    def process_batch(self, reqs: List[SnoozeAlertRequest]):
        """Process a list of alerts"""

    def process_alert(
        self, req: AlertWithURL
    ) -> Optional[SnoozeWebhookResponseContent]:
        """Send a message for a single alert"""
        response = SnoozeWebhookResponseContent()
        for space in req.spaces:
            reminder, thread = find_space_thread(req, space)
            if reminder:
                resp = self.send_reply(req, space, thread)
                thread = resp.get("thread", {}).get("name")
                if thread:
                    response.threads.append(thread)
            else:
                resp = self.send_new_message(req, space)
                thread = resp.get("thread", {}).get("name")
                if thread:
                    response.threads.append(thread)
        return response


def extract_space_thread(spacethread: str) -> Tuple[str, str]:
    split = "/".split(spacethread)
    if len(split) != 4:
        return "", ""
    return f"{split[0]}/{split[1]}", f"{split[2]}/{split[3]}"


def find_space_thread(req: SnoozeAlertRequest, space: str) -> Tuple[bool, str]:
    for resp in req.alert.snooze_webhook_responses:
        for t in resp.content.threads:
            reqspace, thread = extract_space_thread(t)
            if space == reqspace:
                return True, thread
    return False, ""
