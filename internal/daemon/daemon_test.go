package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type fakeRunnable struct {
	err error
}

func (f *fakeRunnable) Run(_ context.Context) error { return f.err }

func cfg(build func(string, *slog.Logger) (Runnable, error)) Config {
	return Config{Name: "snooze-test", DefaultConfig: "/tmp/x.yaml", Build: build}
}

func okBuild(string, *slog.Logger) (Runnable, error) { return &fakeRunnable{}, nil }

func TestRun(t *testing.T) {
	t.Run("version prints to stdout and returns 0", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := run([]string{"version"}, cfg(okBuild), &out, &errOut)
		if code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(out.String(), "snooze-test") {
			t.Fatalf("stdout = %q, want it to contain the binary name", out.String())
		}
	})

	t.Run("unknown flag returns 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := run([]string{"-nope"}, cfg(okBuild), &out, &errOut); code != 2 {
			t.Fatalf("code = %d, want 2", code)
		}
	})

	t.Run("build error returns 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		build := func(string, *slog.Logger) (Runnable, error) { return nil, errors.New("boom") }
		if code := run(nil, cfg(build), &out, &errOut); code != 1 {
			t.Fatalf("code = %d, want 1", code)
		}
	})

	t.Run("run canceled returns 0", func(t *testing.T) {
		var out, errOut bytes.Buffer
		build := func(string, *slog.Logger) (Runnable, error) {
			return &fakeRunnable{err: context.Canceled}, nil
		}
		if code := run(nil, cfg(build), &out, &errOut); code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
	})

	t.Run("run error returns 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		build := func(string, *slog.Logger) (Runnable, error) {
			return &fakeRunnable{err: errors.New("explode")}, nil
		}
		if code := run(nil, cfg(build), &out, &errOut); code != 1 {
			t.Fatalf("code = %d, want 1", code)
		}
	})

	t.Run("subcommand dispatches and returns its code", func(t *testing.T) {
		var out, errOut bytes.Buffer
		c := cfg(okBuild)
		c.Subcommands = map[string]func([]string) int{"authorize": func([]string) int { return 7 }}
		if code := run([]string{"authorize"}, c, &out, &errOut); code != 7 {
			t.Fatalf("code = %d, want 7", code)
		}
	})

	t.Run("-c flag routes to Build", func(t *testing.T) {
		var got string
		build := func(path string, _ *slog.Logger) (Runnable, error) {
			got = path
			return &fakeRunnable{}, nil
		}
		var out, errOut bytes.Buffer
		run([]string{"-c", "/tmp/test.yaml"}, cfg(build), &out, &errOut)
		if got != "/tmp/test.yaml" {
			t.Fatalf("Build got %q, want /tmp/test.yaml", got)
		}
	})

	t.Run("nil Runnable without error returns 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		build := func(string, *slog.Logger) (Runnable, error) { return nil, nil }
		if code := run(nil, cfg(build), &out, &errOut); code != 1 {
			t.Fatalf("code = %d, want 1", code)
		}
	})

	t.Run("default -c value is DefaultConfig", func(t *testing.T) {
		var got string
		build := func(path string, _ *slog.Logger) (Runnable, error) {
			got = path
			return &fakeRunnable{}, nil
		}
		var out, errOut bytes.Buffer
		run(nil, cfg(build), &out, &errOut)
		if got != "/tmp/x.yaml" {
			t.Fatalf("Build got %q, want the DefaultConfig /tmp/x.yaml", got)
		}
	})
}

func TestHandleVersion(t *testing.T) {
	var out bytes.Buffer
	if !HandleVersion("snooze-x", []string{"version"}, &out) {
		t.Fatal("HandleVersion returned false for the version arg")
	}
	if !strings.Contains(out.String(), "snooze-x") {
		t.Fatalf("output = %q, want it to contain the binary name", out.String())
	}
	if HandleVersion("snooze-x", []string{"run"}, &out) {
		t.Fatal("HandleVersion returned true for a non-version arg")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("SNOOZE_TEST_ENVOR", "")
	if got := EnvOr("SNOOZE_TEST_ENVOR", "fallback"); got != "fallback" {
		t.Fatalf("empty env: got %q, want fallback", got)
	}
	t.Setenv("SNOOZE_TEST_ENVOR", "actual")
	if got := EnvOr("SNOOZE_TEST_ENVOR", "fallback"); got != "actual" {
		t.Fatalf("set env: got %q, want actual", got)
	}
}
