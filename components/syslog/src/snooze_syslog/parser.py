"""Module with utils to parse syslog message"""

import logging
import re
from datetime import datetime
from dateutil import parser

SYSLOG_FACILITY_NAMES = [
    "kern",
    "user",
    "mail",
    "daemon",
    "auth",
    "syslog",
    "lpr",
    "news",
    "uucp",
    "cron",
    "authpriv",
    "ftp",
    "ntp",
    "audit",
    "alert",
    "clock",
    "local0",
    "local1",
    "local2",
    "local3",
    "local4",
    "local5",
    "local6",
    "local7",
]

SYSLOG_SEVERITY_NAMES = [
    "emerg",
    "alert",
    "crit",
    "err",
    "warning",
    "notice",
    "info",
    "debug",
]

LOG = logging.getLogger("snooze.syslog.parser")


def decode_priority(pri):
    """Decode the syslog facility and severity from the PRI"""
    facility = pri >> 3
    severity = pri & 7
    return SYSLOG_FACILITY_NAMES[facility], SYSLOG_SEVERITY_NAMES[severity]


def parse_rfc3164(msg):
    """Parse Syslog RFC 3164 message format"""
    regex = (
        r"<(?P<pri>\d{1,3})>"
        + r"(?P<date>\S{3}\s{1,2}\d?\d \d{2}:\d{2}:\d{2}) "
        + r"(?P<host>\S+)"
        + r"(?: (?P<process>\S+?)(?:\[(?P<pid>\d+)\])?:)? "
        + r"(?P<message>.*)"
    )
    match = re.match(regex, msg)
    if match:
        groupdict = match.groupdict()
        record = {
            "syslog_type": "rfc3164",
            "pri": int(groupdict["pri"]),
            "host": groupdict["host"],
            "message": groupdict["message"],
        }

        date_str = groupdict.get("date")
        if date_str is not None:
            date = datetime.strptime(date_str, "%b %d %H:%M:%S")
            date = date.replace(year=datetime.now().year)
            record["timestamp"] = date.astimezone().isoformat()

        process = groupdict.get("process")
        if process:
            record["process"] = process

        pid = groupdict.get("pid")
        if pid:
            record["pid"] = int(pid)

        return record
    else:
        raise Exception("Message doesn't match RFC3164 format")


def parse_rfc5424_struct(structdata):
    """
    Parse the Syslog RFC 5424 struct data string
    Example input:
    '[exampleSDID@9999 a="1" b="2"][exampleSDID@8888 c="3" d="4"]'
    Example output:
    ```
    {
        'structdata': {
            'exampleSDID@9999': {'a': '1', 'b': '2'},
            'exampleSDID@8888': {'c': '3', 'd': '4'}
        }
    }
    ```
    """
    struct = {}
    for data in re.findall(r"\[.*?\]", structdata):
        data = data[1:-1]
        sdid, keyvalues = data.split(" ", 1)
        mystruct = {}
        for keyvalue in re.findall(r'(?P<key>\S+)="(?P<value>.*?)"', keyvalues):
            key, value = keyvalue
            mystruct[key] = value
        struct[sdid] = mystruct
    return struct


def parse_rfc5424(msg):
    """Parse Syslog RFC 5424 message format"""
    regex = (
        r"<(?P<pri>\d+)>1 "
        + r"(?P<timestamp>\S+) "
        + r"(?P<host>\S+) "
        + r"(?P<process>\S+) "
        + r"(?P<pid>\S+) "
        + r"(?P<msgid>\S+) "
        + r"(?P<structs>(?:(?:\[.*?\])+|-) )?"
        + r"(?P<message>.*)"
    )
    match = re.match(regex, msg)
    if match:
        groupdict = match.groupdict()
        record = {
            "syslog_type": "rfc5424",
            "pri": int(groupdict["pri"]),
            "host": groupdict["host"],
            "process": groupdict["process"],
            "msgid": groupdict["msgid"],
            "message": groupdict["message"],
        }

        timestamp = groupdict["timestamp"]
        if timestamp != "-":
            record["timestamp"] = timestamp

        pid = groupdict["pid"]
        if pid != "-":
            record["pid"] = int(pid)

        structdata = groupdict.get("structs")
        if structdata:
            struct_dict = parse_rfc5424_struct(structdata)
            record.update(struct_dict)

        return record
    else:
        raise Exception("Message doesn't match RFC5424 format")


def parse_cisco(msg):
    """Parse Cisco Syslog message format"""
    regex = r"<(?P<pri>\d+)>.*" + r"(%(?P<fsm>[A-Z0-9_-]+)):? " + r"(?P<message>.*)"
    match = re.match(regex, msg)
    if match:
        groupdict = match.groupdict()
        record = {
            "syslog_type": "cisco",
            "pri": int(groupdict["pri"]),
            "message": groupdict["message"],
        }

        fsm = groupdict.get("fsm")
        if fsm:
            try:
                facility, severity, mnemonic = fsm.split("-")
            except ValueError as err:
                LOG.error("Error parsing Cisco FSM for `%s`: %s", fsm, err)
                facility = severity = mnemonic = "na"
        else:
            facility = severity = mnemonic = "na"

        record.update(
            {
                "cisco_facility": facility,
                "cisco_severity": severity,
                "cisco_mnemonic": mnemonic,
            }
        )

        return record
    else:
        raise Exception("Message doesn't match Cisco format")


def parse_rsyslog(msg):
    """Parse Rsyslog's `RSYSLOG_ForwardFormat` format (High precision timestamp format)"""
    regex = (
        r"<(?P<pri>\d+)>"
        + r"(?P<timestamp>\d{4}-\d{2}-\d{2}T.*?) "
        + r"(?P<host>\S+)"
        + r"( (?P<process>\S+?)(?:\[(?P<pid>\d+)\])?:)? "
        + r"(?P<message>.*)"
    )
    match = re.match(regex, msg)
    if match:
        groupdict = match.groupdict()
        timestamp = parser.isoparse(groupdict["timestamp"])

        record = {
            "syslog_type": "rsyslog",
            "pri": int(groupdict["pri"]),
            "host": groupdict["host"],
            "message": groupdict["message"],
            "timestamp": timestamp.astimezone().isoformat(),
        }

        process = groupdict.get("process")
        if process:
            record["process"] = process

        pid = groupdict.get("pid")
        if pid:
            record["pid"] = int(pid)

        return record
    else:
        raise Exception("Message doesn't match Rsyslog format")


def parse_syslog(ipaddr: str, data: bytes, source="syslog"):
    """Parse a syslog message from the queue"""
    LOG.debug("Parsing syslog message...")
    records = list()

    for msg in data.strip().decode().split("\n"):
        try:
            LOG.debug("Found: %s", msg)
            record = dict()

            record["syslog_ip"] = ipaddr

            if not msg or "last message repeated" in msg:
                LOG.debug("Skipping message: %s", msg)
                continue

            if re.match(r"<\d+>1 ", msg):
                record.update(parse_rfc5424(msg))

            elif re.match(r"<(\d{1,3})>\S{3}\s", msg):
                record.update(parse_rfc3164(msg))

            elif re.match(r"<\d+>.*%[A-Z0-9_-]+", msg):
                record.update(parse_cisco(msg))

            elif re.match(r"<\d+>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}", msg):
                record.update(parse_rsyslog(msg))

            else:
                LOG.error("Could not parse message: %s", msg)
                continue

            record["source"] = source
            record["raw"] = msg

            if "timestamp" not in record:
                record["timestamp"] = datetime.now().astimezone().isoformat()

            facility, severity = decode_priority(record["pri"])
            record.update(
                {
                    "facility": facility,
                    "severity": severity,
                }
            )

            records.append(record)
        except Exception as err:
            LOG.error("Error while parsing `%s`: %s", msg, err)
            continue

    return records
