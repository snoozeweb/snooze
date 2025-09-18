from pydantic import BaseModel
from typing import List, Optional


class RetentionSettings(BaseModel):
    state: str


class Thread(BaseModel):
    name: str
    retentionSettings: Optional[RetentionSettings] = None


class Card(BaseModel): ...


class User(BaseModel):
    type: str
    name: str
    displayName: str
    avatarUrl: Optional[str] = None
    domainId: Optional[str] = None
    email: Optional[str] = None


class MembershipCount(BaseModel):
    joinedDirectHumanUserCount: int


class Space(BaseModel):
    name: str
    type: str
    displayName: Optional[str] = None
    spaceThreadingState: str
    spaceType: str
    spaceHistoryState: str
    lastActiveTime: str
    membershipCount: MembershipCount
    spaceUri: str


class SlashCommand(BaseModel):
    commandId: int


class Message(BaseModel):
    name: str
    sender: User
    createTime: str
    text: str = ""
    cards: List[Card] = []
    thread: Thread
    space: Space
    threadReply: bool = False
    argumentText: Optional[str] = None
    messageHistoryState: str
    formattedText: str = ""
    slashCommand: Optional[SlashCommand] = None
    retentionSettings: Optional[RetentionSettings] = None


class Action(BaseModel):
    actionMethodName: str


class TimeZone(BaseModel):
    id: str
    offset: int


class Common(BaseModel):
    userLocale: str
    hostApp: str
    timeZone: Optional[TimeZone] = None
    invokedFunction: Optional[str] = None


class AppCommandMetadata(BaseModel):
    appCommandId: int
    appCommandType: str


class Event(BaseModel):
    """The payload received by the pubsub"""

    type: str
    eventTime: str
    message: Optional[Message] = None
    user: User
    space: Space
    action: Optional[Action] = None
    common: Optional[Common] = None
    thread: Optional[Thread] = None
    appCommandMetadata: Optional[AppCommandMetadata] = None


class SnoozeWebhookResponseContent(BaseModel):
    threads: List[str] = []
    multithreads: List[str] = []


class SnoozeWebhookResponse(BaseModel):
    action_name: str
    content: SnoozeWebhookResponseContent


class NotificationFrom(BaseModel):
    message: str = ""
    name: str = "anonymous"


class SnoozeAlert(BaseModel):
    source: str
    host: str
    process: str
    message: str
    timestamp: str
    uid: str
    rules: List[str]
    plugins: List[str]
    hash: str
    duplicates: int
    notifications: List[str]
    snooze_webhook_responses: List[SnoozeWebhookResponse] = []
    notification_from: Optional[NotificationFrom] = None
    env: Optional[str] = None


class SnoozeAlertRequest(BaseModel):
    alert: SnoozeAlert
    spaces: List[str]


class AlertWithURL(SnoozeAlertRequest):
    url: str
