'''Setup Opentelementry config'''

import logging
import traceback
from enum import Enum
from pathlib import Path
from typing import Optional
from logging import LogRecord

from pydantic import Field
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import SERVICE_NAME, Resource

from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as HttpExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter as GrpcExporter

from snooze.utils.config import ReadOnlyConfig, SNOOZE_CONFIG

MONGODB_TRACER = TracerProvider(resource=Resource(attributes={SERVICE_NAME: 'mongodb'}))

# https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/logs/data-model.md#severity-fields
CONVERT_SEVERITY_TEXT = {
    logging.DEBUG: 'DEBUG',
    logging.INFO: 'INFO',
    logging.WARNING: 'WARNING',
    logging.ERROR: 'ERROR',
    logging.CRITICAL: 'FATAL',
}

# https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/logs/data-model.md#field-severitynumber
CONVERT_SEVERITY_NB = {
    logging.DEBUG: 5,
    logging.INFO: 9,
    logging.WARNING: 13,
    logging.ERROR: 17,
    logging.CRITICAL: 21,
}

# Function used by snooze.logging
def otel_log_hook(span: 'Span', record: LogRecord):
    '''Adding logs to opentelemetry spans'''
    if span and span.is_recording():
        attributes = {
            'Body': record.getMessage(),
            'File': record.pathname,
            'LineNumber': record.lineno,
            'SeverityText': CONVERT_SEVERITY_TEXT.get(record.levelno, ''),
            'SeverityNumber': CONVERT_SEVERITY_NB.get(record.levelno, ''),
        }
        if record.exc_info: # Add the traceback in the body if any
            attributes['Body'] += "\n" + ''.join(traceback.format_exception(*record.exc_info))
        span.add_event(record.name, attributes=attributes)

class Otlp(Enum):
    '''Opentelemetry Protocol'''
    GRPC = 'grpc'
    HTTP = 'http'

class TracingConfig(ReadOnlyConfig):
    '''Tracing configuration'''
    class Config:
        title = 'Opentelemetry tracing configuration'
        section = 'tracing'

    endpoint: Optional[str] = Field(
        title='Endpoint',
        description='`host:port/path` formatted address of the Opentelemetry backend',
        default=None,
    )
    protocol: Otlp = Field(
        title='Protocol',
        description='Opentelemetry protocol to use',
        default='grpc',
    )

def configure_tracer(basedir: Path = SNOOZE_CONFIG):
    '''Setup opentelemetry traces'''
    config = TracingConfig(basedir)
    resource = Resource(attributes={SERVICE_NAME: 'snooze-server'})
    provider = TracerProvider(resource=resource)

    if config.endpoint:
        if config.protocol == Otlp.HTTP:
            exporter = HttpExporter(endpoint=config.endpoint)
        elif config.protocol == Otlp.GRPC:
            exporter = GrpcExporter(endpoint=config.endpoint)
        processor = BatchSpanProcessor(exporter)
        provider.add_span_processor(processor)
        MONGODB_TRACER.add_span_processor(processor)

    trace.set_tracer_provider(provider)
