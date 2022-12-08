'''Logging configuration'''

import logging
import sys
import traceback
from enum import Enum
from logging import getLogger, Formatter, Handler, Logger, StreamHandler, LogRecord
from logging.handlers import TimedRotatingFileHandler, RotatingFileHandler
from pathlib import Path
from typing import Union, Literal, Dict

from opentelemetry.instrumentation.logging import LoggingInstrumentor
from pydantic import BaseModel, Field
from pythonjsonlogger.jsonlogger import JsonFormatter

from snooze.utils.config import ReadOnlyConfig, SNOOZE_CONFIG
from snooze.tracing import otel_log_hook

class LogMode(Enum):
    CONSOLE = 'console'
    FILE = 'file'

class LogLevel(Enum):
    '''A python logging compatible log level'''
    DEBUG = 'DEBUG'
    INFO = 'INFO'
    WARNING = 'WARNING'
    ERROR = 'ERROR'
    CRITICAL = 'CRITICAL'

class LogFormat(Enum):
    '''Describe the target log format'''
    JSON = 'json'
    TEXT = 'text'

class LogCommon(BaseModel):
    '''Log options common to all logger types'''
    level: LogLevel = Field(
        title='Log level',
        description='Log only higher than the set level',
        default=LogLevel.INFO
    )
    fmt: LogFormat = Field(
        title='Log format',
        description='The log output format',
        default=LogFormat.TEXT,
    )

class LogConsole(LogCommon):
    mode: Literal['console'] = 'console'

class LogFile(LogCommon):
    mode: Literal['file'] = 'file'
    logdir: Path = Field(
        title='Logging directory',
        description='Path to the directory used for logging',
        default=Path('/var/log/snooze/server'),
    )

class LogConfig(ReadOnlyConfig):
    '''Logging configuration'''
    class Config:
        title = 'Logging configuration'
        section = 'logging'

    logging: Union[LogConsole, LogFile] = Field(
        title='Logging configuration',
        discriminator='mode',
        default_factory=LogConsole,
    )

def configure_loggers(basedir: Path = SNOOZE_CONFIG):
    '''Configure the main loggers'''

    # Opentelemetry integration (get the traceID and spanID in logs)
    LoggingInstrumentor().instrument(log_hook=otel_log_hook)

    config = LogConfig(basedir)
    loggers = {
        'main': getLogger('snooze'),
        'process': getLogger('snooze-process'),
        'api': getLogger('snooze-api'),
        'audit': getLogger('snooze-audit'),
    }
    if config.logging.mode == 'file':
        handlers = {
            'main': TimedRotatingFileHandler(
                filename=config.logging.logdir / 'main.log',
            ),
            'audit': TimedRotatingFileHandler(
                filename=config.logging.logdir / 'audit.log',
                when='midnight',
                backupCount=10,
            ),
            'api': TimedRotatingFileHandler(
                filename=config.logging.logdir / 'api.log',
                when='midnight',
                backupCount=10,
            ),
            'process': RotatingFileHandler(
                filename=config.logging.logdir / 'process.log',
                maxBytes=100_000_000, # 100MB
                backupCount=5,
            ),
        }
    elif config.logging.mode == 'console':
        handlers = {
            'main': StreamHandler(stream=sys.stdout),
            'audit': StreamHandler(stream=sys.stdout),
            'api': StreamHandler(stream=sys.stdout),
            'process': StreamHandler(stream=sys.stdout),
        }
    else:
        raise RuntimeError(f"Unknown logging mode: {config.logging.mode}")

    add_formatters(handlers, config)

    # Assign handlers to loggers
    for name, logger in loggers.items():
        logger.addHandler(handlers[name])
        logger.setLevel(config.logging.level.value)

    loggers['main'].debug("Log system ON!")
    return loggers['main']

def add_formatters(handlers: Dict[str, Handler], config: LogConfig):
    '''Modify a dict of handlers to add the formatters'''
    if config.logging.fmt == LogFormat.JSON:
        json_fmt = JsonFormatter()
        for handler in handlers.values():
            handler.setFormatter(json_fmt)
    elif config.logging.fmt == LogFormat.TEXT:
        formatters = {
            'main': Formatter("%(asctime)s %(name)-20s %(levelname)-6s %(message)s"),
            'tracing': Formatter(
                "%(asctime)s %(name)-20s %(levelname)-6s "
                "[trace_id=%(otelTraceID)s "
                "span_id=%(otelSpanID)s "
                "resource.service.name=%(otelServiceName)s] "
                "%(message)s"
            ),
        }
        for name, handler in handlers.items():
            if name in ['process', 'api', 'tracing']:
                handler.setFormatter(formatters['tracing'])
            else:
                handler.setFormatter(formatters['main'])
    # Configure log level
    for handler in handlers.values():
        handler.setLevel(config.logging.level.value)
