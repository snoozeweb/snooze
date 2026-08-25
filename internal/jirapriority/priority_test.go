package jirapriority

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// frenchScheme mirrors a real 4-entry French JIRA Cloud site: names localized,
// ids stable, ordered most severe first.
var frenchScheme = Scheme{
	{ID: "1", Name: "Critique"},
	{ID: "2", Name: "Grave"},
	{ID: "3", Name: "Moyen"},
	{ID: "4", Name: "Faible"},
}

// englishScheme is the JIRA-default 5-entry scheme.
var englishScheme = Scheme{
	{ID: "1", Name: "Highest"},
	{ID: "2", Name: "High"},
	{ID: "3", Name: "Medium"},
	{ID: "4", Name: "Low"},
	{ID: "5", Name: "Lowest"},
}

func TestForSeverity_positionalMapping(t *testing.T) {
	cases := []struct {
		severity string
		french   string // expected name in the 4-entry scheme
		english  string // expected name in the 5-entry scheme
	}{
		{"emergency", "Critique", "Highest"},
		{"emerg", "Critique", "Highest"},
		{"alert", "Critique", "High"},
		{"critical", "Grave", "High"},
		{"crit", "Grave", "High"},
		{"err", "Grave", "Medium"},
		{"warning", "Moyen", "Medium"},
		{"notice", "Moyen", "Low"},
		{"info", "Moyen", "Low"},
		{"debug", "Faible", "Lowest"},
		{"ok", "Faible", "Lowest"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			got, ok := frenchScheme.ForSeverity(tc.severity)
			require.True(t, ok)
			require.Equal(t, tc.french, got.Name, "4-entry scheme")

			got, ok = englishScheme.ForSeverity(tc.severity)
			require.True(t, ok)
			require.Equal(t, tc.english, got.Name, "5-entry scheme")
		})
	}
}

// The mapping is monotonic: a more severe alert never gets a lower priority.
func TestForSeverity_monotonic(t *testing.T) {
	ladder := []string{"emergency", "alert", "critical", "err", "warning", "notice", "info", "debug", "ok"}
	for _, sch := range []Scheme{frenchScheme, englishScheme, {{ID: "9", Name: "Only"}}} {
		prev := -1
		for _, sev := range ladder {
			p, ok := sch.ForSeverity(sev)
			require.True(t, ok, sev)
			idx := -1
			for i, e := range sch {
				if e.ID == p.ID {
					idx = i
				}
			}
			require.GreaterOrEqual(t, idx, prev, "severity %q went backwards", sev)
			prev = idx
		}
	}
}

func TestForSeverity_unrankedAndEmpty(t *testing.T) {
	// "major"/"minor" are not on Snooze's ladder: no guess, caller omits the field.
	_, ok := frenchScheme.ForSeverity("major")
	require.False(t, ok)
	_, ok = frenchScheme.ForSeverity("")
	require.False(t, ok)
	_, ok = Scheme{}.ForSeverity("critical")
	require.False(t, ok)
}

func TestResolve_precedence(t *testing.T) {
	cases := []struct {
		name     string
		override string
		severity string
		wantID   string
		wantSrc  string
	}{
		{name: "id override wins", override: "4", severity: "critical", wantID: "4", wantSrc: "override-id"},
		{name: "name override, exact", override: "Grave", severity: "info", wantID: "2", wantSrc: "override-name"},
		{name: "name override, case-insensitive", override: "  moyen ", severity: "info", wantID: "3", wantSrc: "override-name"},
		{name: "english override on french site falls back", override: "Highest", severity: "critical", wantID: "2", wantSrc: "severity"},
		{name: "no override", override: "", severity: "warning", wantID: "3", wantSrc: "severity"},
		{name: "unresolvable override, unranked severity", override: "Nope", severity: "major", wantID: "", wantSrc: "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := frenchScheme.Resolve(tc.override, tc.severity)
			require.Equal(t, tc.wantSrc, got.Source)
			require.Equal(t, tc.wantID, got.Priority.ID)
		})
	}
}

// The wire form is always the id — never the localized name.
func TestResolution_Field(t *testing.T) {
	r := frenchScheme.Resolve("", "critical")
	require.Equal(t, map[string]any{"id": "2"}, r.Field())
	require.Nil(t, Resolution{Source: "none"}.Field())
}

func TestParseCreateMeta(t *testing.T) {
	raw := []byte(`{"projects":[{"issuetypes":[
      {"id":"10001","fields":{"priority":{"allowedValues":[
        {"id":"9","name":"Other"}]}}},
      {"id":"10083","fields":{"priority":{"allowedValues":[
        {"id":"1","name":"Critique"},{"id":"2","name":"Grave"},
        {"id":"3","name":"Moyen"},{"id":"4","name":"Faible"}]}}}
    ]}]}`)

	got, err := ParseCreateMeta(raw, "10083")
	require.NoError(t, err)
	require.Equal(t, frenchScheme, got)

	// No issue type named: first one carrying a priority field wins.
	got, err = ParseCreateMeta(raw, "")
	require.NoError(t, err)
	require.Equal(t, Scheme{{ID: "9", Name: "Other"}}, got)

	// Priority not on the create screen → empty scheme, no error, so the
	// caller can fall back to the global list.
	got, err = ParseCreateMeta([]byte(`{"projects":[{"issuetypes":[{"id":"1","fields":{}}]}]}`), "1")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = ParseCreateMeta([]byte(`not json`), "")
	require.Error(t, err)
}

func TestParsePriorityList(t *testing.T) {
	flat, err := ParsePriorityList([]byte(
		`[{"id":"1","name":"Critique"},{"id":"2","name":"Grave"},{"id":"3","name":"Moyen"},{"id":"4","name":"Faible"}]`))
	require.NoError(t, err)
	require.Equal(t, frenchScheme, flat)

	paged, err := ParsePriorityList([]byte(
		`{"values":[{"id":"1","name":"Critique"},{"id":"2","name":"Grave"},{"id":"3","name":"Moyen"},{"id":"4","name":"Faible"}]}`))
	require.NoError(t, err)
	require.Equal(t, frenchScheme, paged)

	_, err = ParsePriorityList([]byte(`{"nope":1}`))
	require.NoError(t, err) // decodes, just empty

	_, err = ParsePriorityList([]byte(`garbage`))
	require.Error(t, err)
}

func TestCache_fetchesOnceThenExpires(t *testing.T) {
	var calls int
	now := time.Unix(1_700_000_000, 0)
	c := NewCache(time.Minute)
	c.now = func() time.Time { return now }
	fetch := func(context.Context) (Scheme, error) { calls++; return frenchScheme, nil }

	for i := 0; i < 3; i++ {
		got, err := c.Get(context.Background(), "CG/10083", fetch)
		require.NoError(t, err)
		require.Equal(t, frenchScheme, got)
	}
	require.Equal(t, 1, calls)

	// A different key is fetched separately.
	_, err := c.Get(context.Background(), "OPS/10001", fetch)
	require.NoError(t, err)
	require.Equal(t, 2, calls)

	// Past the TTL the scheme is refetched.
	now = now.Add(2 * time.Minute)
	_, err = c.Get(context.Background(), "CG/10083", fetch)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestCache_invalidateForcesRefetch(t *testing.T) {
	var calls int
	c := NewCache(time.Hour)
	fetch := func(context.Context) (Scheme, error) { calls++; return frenchScheme, nil }

	_, err := c.Get(context.Background(), "CG/10083", fetch)
	require.NoError(t, err)
	c.Invalidate("CG/10083")
	_, err = c.Get(context.Background(), "CG/10083", fetch)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestCache_errorsAreNotCached(t *testing.T) {
	var calls int
	c := NewCache(time.Hour)
	boom := errors.New("boom")
	fetch := func(context.Context) (Scheme, error) {
		calls++
		if calls == 1 {
			return nil, boom
		}
		return frenchScheme, nil
	}
	_, err := c.Get(context.Background(), "k", fetch)
	require.ErrorIs(t, err, boom)
	got, err := c.Get(context.Background(), "k", fetch)
	require.NoError(t, err)
	require.Equal(t, frenchScheme, got)

	// An empty scheme is an error too — caching it would wedge every create.
	empty := NewCache(time.Hour)
	_, err = empty.Get(context.Background(), "k", func(context.Context) (Scheme, error) { return nil, nil })
	require.Error(t, err)
}

// Concurrent misses on the same key must fetch once and not race.
func TestCache_concurrentGet(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	c := NewCache(time.Hour)
	fetch := func(context.Context) (Scheme, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return frenchScheme, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Get(context.Background(), "CG/10083", fetch)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Equal(t, 1, calls)
}

// A nil Cache is usable: it just doesn't cache.
func TestCache_nilReceiver(t *testing.T) {
	var c *Cache
	var calls int
	fetch := func(context.Context) (Scheme, error) { calls++; return frenchScheme, nil }
	for i := 0; i < 2; i++ {
		got, err := c.Get(context.Background(), "k", fetch)
		require.NoError(t, err)
		require.Equal(t, frenchScheme, got)
	}
	require.Equal(t, 2, calls)
	c.Invalidate("k") // must not panic
}
