"""A package with all the text formatters for the messages"""

from snooze_googlechat.types import SnoozeAlertRequest

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
    msg += f"[{req.alert.source}] "
    msg += f"{req.alert.process} {req.alert.message}"
    return msg
