package k8sevents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// baseConfig returns a minimal valid (post-defaults) config for mapping tests.
// It uses an explicit apiserver + insecure so WithDefaults doesn't try to
// auto-detect in-cluster material.
func baseConfig(t *testing.T) Config {
	t.Helper()
	c, err := Config{
		Server:      "http://snooze.example",
		APIServer:   "https://10.0.0.1:6443",
		K8sToken:    "k8s-tok",
		K8sInsecure: true,
	}.WithDefaults()
	require.NoError(t, err)
	return c
}

func TestConfig_WithDefaults(t *testing.T) {
	t.Run("requires server", func(t *testing.T) {
		_, err := Config{APIServer: "https://x", K8sToken: "t", K8sInsecure: true}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("explicit apiserver needs token", func(t *testing.T) {
		_, err := Config{Server: "http://x", APIServer: "https://x", K8sInsecure: true}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("explicit apiserver needs ca or insecure", func(t *testing.T) {
		_, err := Config{Server: "http://x", APIServer: "https://x", K8sToken: "t"}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("fills defaults", func(t *testing.T) {
		c := baseConfig(t)
		require.Equal(t, "local", c.Method)
		require.Equal(t, 30*time.Minute, c.ResyncInterval)
		require.Equal(t, time.Minute, c.DedupWindow)
		require.Equal(t, 30*time.Second, c.RequestTimeout)
		require.Equal(t, "https://10.0.0.1:6443", c.APIServer)
	})
	t.Run("reason overrides merge over defaults", func(t *testing.T) {
		c, err := Config{
			Server: "http://x", APIServer: "https://x", K8sToken: "t", K8sInsecure: true,
			Reasons: map[string]string{"FailedScheduling": "Critical", "MyReason": "WARNING"},
		}.WithDefaults()
		require.NoError(t, err)
		// Override wins and is lower-cased.
		require.Equal(t, "critical", c.Reasons["FailedScheduling"])
		require.Equal(t, "warning", c.Reasons["MyReason"])
		// Untouched default survives.
		require.Equal(t, "critical", c.Reasons["OOMKilling"])
	})
}

func TestConfig_InClusterAutodetect(t *testing.T) {
	dir := t.TempDir()
	tokenPath := dir + "/token"
	caPath := dir + "/ca.crt"
	require.NoError(t, writeFile(tokenPath, []byte("incluster-token\n")))
	require.NoError(t, writeFile(caPath, []byte("dummy")))

	// Point the in-cluster mount points at our temp files and set the env.
	oldTok, oldCA := inClusterTokenFile, inClusterCAFile
	inClusterTokenFile, inClusterCAFile = tokenPath, caPath
	t.Cleanup(func() { inClusterTokenFile, inClusterCAFile = oldTok, oldCA })
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	c, err := Config{Server: "http://snooze"}.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://10.96.0.1:443", c.APIServer)
	require.Equal(t, tokenPath, c.K8sTokenFile)
	require.Equal(t, caPath, c.CACert)
}

func TestConfig_InClusterMissingEnv(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	_, err := Config{Server: "http://snooze"}.WithDefaults()
	require.Error(t, err)
}

func TestWantsType(t *testing.T) {
	warnOnly := baseConfig(t)
	require.True(t, warnOnly.wantsType("Warning"))
	require.False(t, warnOnly.wantsType("Normal"))

	withNormal := warnOnly
	withNormal.IncludeNormal = true
	require.True(t, withNormal.wantsType("Normal"))
	require.True(t, withNormal.wantsType("Warning"))

	explicit := warnOnly
	explicit.EventTypes = []string{"Normal"}
	require.True(t, explicit.wantsType("Normal"))
	require.False(t, explicit.wantsType("Warning")) // EventTypes wins over default
}

func TestSeverityFor(t *testing.T) {
	c := baseConfig(t)

	// Plain Warning → warning.
	require.Equal(t, "warning", c.severityFor(Event{Type: "Warning", Reason: "SomethingMildlyOff"}))
	// Normal → info.
	require.Equal(t, "info", c.severityFor(Event{Type: "Normal", Reason: "Scheduled"}))
	// Elevated reasons.
	require.Equal(t, "critical", c.severityFor(Event{Type: "Warning", Reason: "OOMKilling"}))
	require.Equal(t, "critical", c.severityFor(Event{Type: "Normal", Reason: "Killing"})) // reason wins over type
	require.Equal(t, "error", c.severityFor(Event{Type: "Warning", Reason: "FailedScheduling"}))
	require.Equal(t, "error", c.severityFor(Event{Type: "Warning", Reason: "BackOff"}))
	require.Equal(t, "error", c.severityFor(Event{Type: "Warning", Reason: "CrashLoopBackOff"}))
}

func TestToRecord_Mapping(t *testing.T) {
	c := baseConfig(t)
	ts := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	ev := Event{
		Metadata:            objectMeta{Namespace: "kube-system", Name: "pod-x.123", ResourceVersion: "5012"},
		InvolvedObject:      objectReference{Kind: "Pod", Name: "nginx-7d", Namespace: "prod"},
		Reason:              "BackOff",
		Message:             "Back-off restarting failed container",
		Type:                "Warning",
		LastTimestamp:       ts,
		Count:               7,
		Source:              eventSource{Component: "kubelet", Host: "node-1"},
		ReportingController: "kubelet",
	}
	rec := c.ToRecord(ev)
	require.Equal(t, "kubernetes", rec.Source)
	require.Equal(t, "nginx-7d", rec.Host)
	require.Equal(t, "Pod/BackOff", rec.Process)
	require.Equal(t, "error", rec.Severity)
	require.Equal(t, "Back-off restarting failed container", rec.Message)
	require.Equal(t, "prod", rec.Environment)
	require.Equal(t, ts, rec.Timestamp)

	require.Equal(t, "prod", rec.Raw["namespace"])
	require.Equal(t, "BackOff", rec.Raw["reason"])
	require.Equal(t, 7, rec.Raw["count"])
	io, ok := rec.Raw["involved_object"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Pod", io["kind"])
	require.Equal(t, "nginx-7d", io["name"])
	src, ok := rec.Raw["source"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "kubelet", src["component"])
}

func TestToRecord_HostFallbackToSource(t *testing.T) {
	c := baseConfig(t)
	ev := Event{
		InvolvedObject: objectReference{Kind: "Node"}, // no name
		Reason:         "NodeNotReady",
		Type:           "Warning",
		Source:         eventSource{Host: "node-7"},
	}
	rec := c.ToRecord(ev)
	require.Equal(t, "node-7", rec.Host) // fell back to source.host
	require.Equal(t, "Node/NodeNotReady", rec.Process)
	require.Equal(t, "critical", rec.Severity)
}

func TestEvent_DecodeFromAPIServerJSON(t *testing.T) {
	// A real-ish watch object body to confirm camelCase field names decode.
	body := []byte(`{
		"metadata":{"namespace":"default","name":"web.17a","resourceVersion":"9001","uid":"u1"},
		"involvedObject":{"kind":"Pod","name":"web-0","namespace":"default","uid":"u2"},
		"reason":"OOMKilling","message":"Memory cgroup out of memory","type":"Warning",
		"lastTimestamp":"2026-05-27T09:00:00Z","count":3,
		"source":{"component":"kubelet","host":"node-3"},"reportingComponent":"kubelet"
	}`)
	var ev Event
	require.NoError(t, json.Unmarshal(body, &ev))
	require.Equal(t, "OOMKilling", ev.Reason)
	require.Equal(t, "Pod", ev.InvolvedObject.Kind)
	require.Equal(t, "web-0", ev.InvolvedObject.Name)
	require.Equal(t, 3, ev.Count)
	require.Equal(t, "9001", ev.Metadata.ResourceVersion)
	require.Equal(t, "node-3", ev.Source.Host)
	require.Equal(t, 2026, ev.LastTimestamp.UTC().Year())
}
