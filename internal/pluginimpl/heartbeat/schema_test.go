package heartbeat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseHeartbeatMaxLatency: a document carrying max_latency (as the JSON
// float64 every decoder produces) is parsed into hb.MaxLatency.
func TestParseHeartbeatMaxLatency(t *testing.T) {
	hb, ok := parseHeartbeat(map[string]any{
		"name":        "hb1",
		"interval":    float64(60),
		"max_latency": float64(2000),
	})
	require.True(t, ok)
	require.Equal(t, int64(2000), hb.MaxLatency)
}

// TestParseHeartbeatNoMaxLatency: a document without max_latency leaves it 0
// (feature disabled).
func TestParseHeartbeatNoMaxLatency(t *testing.T) {
	hb, ok := parseHeartbeat(map[string]any{
		"name":     "hb1",
		"interval": float64(60),
	})
	require.True(t, ok)
	require.Equal(t, int64(0), hb.MaxLatency)
}

// TestParseHeartbeatLastLatency: a document carrying last_latency is parsed
// into hb.LastLatency.
func TestParseHeartbeatLastLatency(t *testing.T) {
	hb, ok := parseHeartbeat(map[string]any{
		"name":         "hb1",
		"interval":     float64(60),
		"last_latency": float64(350),
	})
	require.True(t, ok)
	require.Equal(t, int64(350), hb.LastLatency)
}

// TestValidateRejectsNegativeMaxLatency: a negative max_latency is rejected.
func TestValidateRejectsNegativeMaxLatency(t *testing.T) {
	p := newPlugin(t, newHost())
	require.Error(t, p.Validate(map[string]any{
		"name":        "x",
		"interval":    float64(60),
		"max_latency": float64(-1),
	}))
}

// TestSchemaIncludesMaxLatency: the schema advertises max_latency and a
// read-only last_latency property.
func TestSchemaIncludesMaxLatency(t *testing.T) {
	p := newPlugin(t, newHost())
	schema, ok := p.Schema().(map[string]any)
	require.True(t, ok)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, props, "max_latency", "schema must expose max_latency")
	require.Contains(t, props, "last_latency", "schema must expose last_latency")

	last, ok := props["last_latency"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, last["readOnly"], "last_latency must be readOnly")
}
