package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInit_BuildsFourScopedLoggers(t *testing.T) {
	var buf bytes.Buffer
	loggers, err := Init(LoggerConfig{Level: "debug", Format: LogFormatJSON, Output: &buf})
	require.NoError(t, err)
	require.NotNil(t, loggers.Snooze)
	require.NotNil(t, loggers.Process)
	require.NotNil(t, loggers.API)
	require.NotNil(t, loggers.Audit)

	loggers.Snooze.Info("hello-main")
	loggers.Process.Info("hello-process")
	loggers.API.Info("hello-api")
	loggers.Audit.Info("hello-audit")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 4)
	wantNames := []string{"snooze", "snooze-process", "snooze-api", "snooze-audit"}
	wantMsgs := []string{"hello-main", "hello-process", "hello-api", "hello-audit"}
	for i, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "line %d not JSON: %s", i, line)
		require.Equal(t, wantNames[i], entry["logger"])
		require.Equal(t, wantMsgs[i], entry["msg"])
	}
}

func TestInit_UnknownLevelErrors(t *testing.T) {
	_, err := Init(LoggerConfig{Level: "shouty"})
	require.Error(t, err)
}

func TestInit_DefaultLevelIsInfo(t *testing.T) {
	var buf bytes.Buffer
	loggers, err := Init(LoggerConfig{Output: &buf})
	require.NoError(t, err)
	loggers.Snooze.Debug("hidden")
	loggers.Snooze.Info("visible")
	require.NotContains(t, buf.String(), "hidden")
	require.Contains(t, buf.String(), "visible")
}

func TestInit_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	loggers, err := Init(LoggerConfig{Format: LogFormatText, Output: &buf})
	require.NoError(t, err)
	loggers.API.Warn("careful")
	out := buf.String()
	require.Contains(t, out, "careful")
	require.Contains(t, out, "logger=snooze-api")
}

func TestWithRequest_AttachesIDs(t *testing.T) {
	var buf bytes.Buffer
	loggers, err := Init(LoggerConfig{Format: LogFormatJSON, Output: &buf})
	require.NoError(t, err)

	ctx := WithRequestID(context.Background(), "req-1")
	ctx = WithTraceID(ctx, "trace-1")
	ctx = WithSpanID(ctx, "span-1")

	l := WithRequest(ctx, loggers.API)
	l.Info("processed")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "req-1", entry["request_id"])
	require.Equal(t, "trace-1", entry["trace_id"])
	require.Equal(t, "span-1", entry["span_id"])
}

func TestWithRequest_NoContextValuesIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	loggers, err := Init(LoggerConfig{Format: LogFormatJSON, Output: &buf})
	require.NoError(t, err)
	l := WithRequest(context.Background(), loggers.Snooze)
	l.Info("plain")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	_, hasReq := entry["request_id"]
	_, hasTrace := entry["trace_id"]
	require.False(t, hasReq)
	require.False(t, hasTrace)
}

func TestContextHelpers_Roundtrip(t *testing.T) {
	ctx := context.Background()
	require.Empty(t, RequestIDFrom(ctx))
	require.Empty(t, TraceIDFrom(ctx))
	require.Empty(t, SpanIDFrom(ctx))

	ctx = WithRequestID(ctx, "r")
	ctx = WithTraceID(ctx, "t")
	ctx = WithSpanID(ctx, "s")
	require.Equal(t, "r", RequestIDFrom(ctx))
	require.Equal(t, "t", TraceIDFrom(ctx))
	require.Equal(t, "s", SpanIDFrom(ctx))
}
