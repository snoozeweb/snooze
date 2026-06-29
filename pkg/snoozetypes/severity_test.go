package snoozetypes

import "testing"

func TestSeverityRank_Known(t *testing.T) {
	if r, ok := SeverityRank("CRITICAL"); r != 2 || !ok {
		t.Fatalf("SeverityRank(\"CRITICAL\") = (%d, %v); want (2, true)", r, ok)
	}
	if r, ok := SeverityRank(" warn "); r != 4 || !ok {
		t.Fatalf("SeverityRank(\" warn \") = (%d, %v); want (4, true)", r, ok)
	}
}

func TestSeverityRank_Unknown(t *testing.T) {
	if r, ok := SeverityRank("p1"); r != 0 || ok {
		t.Fatalf("SeverityRank(\"p1\") = (%d, %v); want (0, false)", r, ok)
	}
}

func TestSeverityVariant_Buckets(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"crit", "critical"},
		{"critical", "critical"},
		{"emerg", "critical"},
		{"err", "error"},
		{"error", "error"},
		{"warn", "warning"},
		{"notice", "info"},
		{"info", "info"},
		{"debug", "info"},
		{"ok", "ok"},
		{"success", "ok"},
		{"p1", "muted"},
	}
	for _, c := range cases {
		if got := SeverityVariant(c.in); got != c.want {
			t.Errorf("SeverityVariant(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeSeverity(t *testing.T) {
	if got := NormalizeSeverity("WARNING "); got != "warning" {
		t.Errorf("NormalizeSeverity(\"WARNING \") = %q; want %q", got, "warning")
	}
	if got := NormalizeSeverity("P1"); got != "p1" {
		t.Errorf("NormalizeSeverity(\"P1\") = %q; want %q", got, "p1")
	}
}

func TestCompareSeverity(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"critical", "warning", -1},
		{"ok", "critical", +1},
		{"ok", "success", 0},
		{"p1", "critical", 0},
		{"critical", "p1", 0},
	}
	for _, c := range cases {
		if got := CompareSeverity(c.a, c.b); got != c.want {
			t.Errorf("CompareSeverity(%q, %q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestLadderMirrorsFrontend is a guard test: it asserts the Go ladder's key set
// and rank values match the canonical syslog-anchored list mirrored from the
// frontend web/src/lib/format/severity-color.ts RANK map. The expected map is
// hardcoded (NOT parsed from the TS file) so any edit to DefaultSeverityRank is
// a conscious choice. Plan 28 establishes the runtime authority.
func TestLadderMirrorsFrontend(t *testing.T) {
	want := map[string]int{
		"emerg": 0, "emergency": 0, "panic": 0,
		"alert": 1,
		"crit":  2, "critical": 2, "fatal": 2,
		"err": 3, "error": 3, "fail": 3, "failure": 3,
		"warn": 4, "warning": 4,
		"notice": 5,
		"info":   6, "informational": 6,
		"debug": 7,
		"ok":    8, "okay": 8, "success": 8,
	}
	if len(DefaultSeverityRank) != len(want) {
		t.Fatalf("DefaultSeverityRank has %d keys; want %d", len(DefaultSeverityRank), len(want))
	}
	for k, v := range want {
		got, ok := DefaultSeverityRank[k]
		if !ok {
			t.Errorf("DefaultSeverityRank missing key %q", k)
			continue
		}
		if got != v {
			t.Errorf("DefaultSeverityRank[%q] = %d; want %d", k, got, v)
		}
	}
	for k := range DefaultSeverityRank {
		if _, ok := want[k]; !ok {
			t.Errorf("DefaultSeverityRank has unexpected key %q", k)
		}
	}
}
