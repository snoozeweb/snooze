package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantCreate_PostsCorrectBody(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/tenant", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"added":["acme"]}`))
	}))
	defer srv.Close()

	rt, out, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "tenant", "create", "--id", "acme", "--display-name", "Acme Corp")
	require.NoError(t, err)
	require.Equal(t, "acme", captured["id"])
	require.Equal(t, "Acme Corp", captured["display_name"])
	_ = out
}

func TestTenantCreate_MissingID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server must not be called")
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	_, _, err := executeCmd(t, rt, "tenant", "create", "--display-name", "No ID")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")
}

func TestTenantList_PrintsTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/tenant", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"acme","display_name":"Acme","status":"active"},{"id":"beta","display_name":"Beta","status":"suspended"}]}`))
	}))
	defer srv.Close()

	rt, out, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "tenant", "list")
	require.NoError(t, err)
	require.Contains(t, out.String(), "acme")
	require.Contains(t, out.String(), "Acme")
	require.Contains(t, out.String(), "active")
	require.Contains(t, out.String(), "beta")
	require.Contains(t, out.String(), "suspended")
}

func TestTenantList_JSONFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"acme","display_name":"Acme","status":"active"}]}`))
	}))
	defer srv.Close()

	rt, out, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	rt.flags.JSON = true

	_, _, err := executeCmd(t, rt, "--json", "tenant", "list")
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, "acme", got[0]["id"])
}

func TestTenantDelete_SendsDelete(t *testing.T) {
	var deletedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		deletedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":1}`))
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "tenant", "delete", "acme")
	require.NoError(t, err)
	require.Equal(t, "/api/v1/tenant/acme", deletedPath)
}

func TestTenantDelete_MissingArg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	_, _, err := executeCmd(t, rt, "tenant", "delete")
	require.Error(t, err)
}

func TestTenantRootHelp_IncludesTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	rt, out, _ := newTestRuntime(t, srv)
	root := NewRootCmd(rt)
	root.SetArgs([]string{"tenant", "--help"})
	_ = root.Execute()
	require.Contains(t, out.String(), "create")
	require.Contains(t, out.String(), "list")
	require.Contains(t, out.String(), "delete")
}
