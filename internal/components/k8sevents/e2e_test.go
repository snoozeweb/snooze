package k8sevents

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestK8sEventsE2E talks to a real kube-apiserver, listing a single page of
// events (watch=false&limit=1) and asserting it returns 200 and parses. It is
// skipped unless SNOOZE_E2E_K8S_APISERVER and SNOOZE_E2E_K8S_TOKEN are set.
//
// Env vars:
//
//	SNOOZE_E2E_K8S_APISERVER  required — e.g. https://10.0.0.1:6443
//	SNOOZE_E2E_K8S_TOKEN      required — a bearer token with get/list on events
//	SNOOZE_E2E_K8S_CA         optional — path to the apiserver CA PEM
//	SNOOZE_E2E_K8S_INSECURE   optional — "true" to skip TLS verification
func TestK8sEventsE2E(t *testing.T) {
	apiserver := os.Getenv("SNOOZE_E2E_K8S_APISERVER")
	token := os.Getenv("SNOOZE_E2E_K8S_TOKEN")
	if apiserver == "" || token == "" {
		t.Skip("set SNOOZE_E2E_K8S_APISERVER and SNOOZE_E2E_K8S_TOKEN to run the k8s-events end-to-end test")
	}
	insecure, _ := strconv.ParseBool(os.Getenv("SNOOZE_E2E_K8S_INSECURE"))

	cfg := Config{
		Server:      "http://unused.example", // not contacted by this test
		APIServer:   apiserver,
		K8sToken:    token,
		CACert:      os.Getenv("SNOOZE_E2E_K8S_CA"),
		K8sInsecure: insecure,
	}
	cfg, err := cfg.WithDefaults()
	require.NoError(t, err)

	client, err := buildAPIClient(cfg)
	require.NoError(t, err)
	client.Timeout = 15 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	u := cfg.APIServer + "/api/v1/events?watch=false&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+cfg.K8sToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var list struct {
		Kind     string  `json:"kind"`
		Items    []Event `json:"items"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	require.Equal(t, "EventList", list.Kind)
	t.Logf("listed %d event(s), resourceVersion=%s", len(list.Items), list.Metadata.ResourceVersion)
}
