import re

from snooze_googlechat.types import SnoozeAlertRequest, AlertWithURL

# Snooze color scheme
GREEN = {"red": 0, "green": 200 / 255, "blue": 83 / 255}  # hex=#00c853
BLUE = {"red": 33 / 255, "green": 150 / 255, "blue": 243 / 255}  # hex=#2196f3
YELLOW = {"red": 255 / 255, "green": 193 / 255, "blue": 7 / 255}  # hex=#ffc107
RED = {"red": 244 / 255, "green": 67 / 255, "blue": 54 / 255}  # hex=#f44336

WARN = "⚠️"


def notification_from(req: SnoozeAlertRequest) -> str:
    """Format notification-from header"""
    msg = ""
    if req.alert.notification_from:
        msg = f"From `{req.alert.notification_from.name}`"
        if req.alert.notification_from.message:
            msg += f": {req.alert.notification_from.message}"
        msg += "\n\n"
    return msg


def reminder(req: SnoozeAlertRequest) -> str:
    """Format an alert reminder"""
    msg = ""
    msg += notification_from(req)
    msg += f"{WARN} <b>New escalation</b> {WARN}\n"
    msg += f"<b>Date</b>: {req.alert.timestamp}"
    return msg


def new_alert(req: SnoozeAlertRequest) -> str:
    """Format a new alert (single)"""
    msg = ""
    msg += notification_from(req)
    msg += f"{req.alert.host} "
    msg += f"{req.alert.process} {req.alert.message}"
    return msg


def get_source_icon(source: str) -> dict:
    if source == "syslog":
        return {"materialIcon": {"name": "list"}}
    elif source == "icinga":
        return {"iconUrl": "https://avatars.githubusercontent.com/u/835441?s=200&v=4"}
    elif source == "nagios":
        return {"iconUrl": "https://avatars.githubusercontent.com/u/5666660?s=200&v=4"}
    elif source == "prometheus":
        return {"iconUrl": "https://avatars.githubusercontent.com/u/3380462?s=200&v=4"}
    else:
        return {"materialIcon": {"name": "add-triangle"}}


def header(req: SnoozeAlertRequest) -> str:
    # Environment colored prefix
    env_str = ""
    if req.alert.env:
        color = "#2196f3"
        if re.match(r"prod", req.alert.env):
            color = "#f44336"
        elif re.match(r"uat|test", req.alert.env):
            color = "#ed9600"
        elif re.match(r"dev|poc", req.alert.env):
            color = "#00c853"
        env_str = f'[<font color="{color}">{req.alert.env}</font>] '

    return f"{env_str}<b>{req.alert.host}</b> <i>{req.alert.process}</i>"


def new_card_v2(req: AlertWithURL) -> dict:
    url = req.url + f"/web/?#record?tab=All&s=hash%3D{req.alert.hash}"
    button_list = {
        "buttons": [
            {
                "text": "Ack",
                "type": "OUTLINED",
                "icon": {"materialIcon": {"name": "Check"}},
                "color": GREEN,
                "altText": "Acknowledge the alert",
                "onClick": {"action": {"function": "ACK"}},
            },
            {
                "icon": {"materialIcon": {"name": "open_in_new"}},
                "altText": "Open the alert in snooze",
                "onClick": {"openLink": {"url": url}},
            },
        ]
    }

    text_section = {
        "widgets": [
            {
                "decoratedText": {
                    "startIcon": get_source_icon(req.alert.source),
                    "text": header(req),
                }
            },
            {
                "textParagraph": {
                    "text": req.alert.message,
                }
            },
            {
                "buttonList": button_list,
            },
        ]
    }

    card = {
        "sections": [text_section],
    }

    cardv2 = {"card_id": "new_alert", "card": card}

    return cardv2
