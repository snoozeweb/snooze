package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

// fakePlugin is a minimal Plugin that records the meta it was built with.
type fakePlugin struct {
	meta     Metadata
	post     int
	reload   int
	procImpl func(ctx context.Context, rec snoozetypes.Record) (Result, error)
}

func (f *fakePlugin) Name() string                              { return f.meta.Name }
func (f *fakePlugin) Metadata() Metadata                        { return f.meta }
func (f *fakePlugin) PostInit(ctx context.Context, h Host) error { f.post++; return nil }
func (f *fakePlugin) Reload(ctx context.Context) error          { f.reload++; return nil }

// fakeProcessor adds Process to fakePlugin to satisfy Processor.
type fakeProcessor struct {
	fakePlugin
}

func (f *fakeProcessor) Process(ctx context.Context, rec snoozetypes.Record) (Result, error) {
	if f.procImpl != nil {
		return f.procImpl(ctx, rec)
	}
	return Result{Action: ActionContinue, Record: rec}, nil
}

func newFake(name string) Factory {
	return func(meta Metadata) (Plugin, error) {
		return &fakePlugin{meta: meta}, nil
	}
}

func newFakeProcessor(name string) Factory {
	return func(meta Metadata) (Plugin, error) {
		return &fakeProcessor{fakePlugin: fakePlugin{meta: meta}}, nil
	}
}

func TestRegister_BasicAndSorted(t *testing.T) {
	resetForTest()
	Register("zeta", []byte("name: zeta\n"), newFake("zeta"))
	Register("alpha", []byte("name: alpha\n"), newFake("alpha"))
	Register("mike", []byte("name: mike\n"), newFake("mike"))

	require.Equal(t, []string{"alpha", "mike", "zeta"}, Registered())
}

func TestRegister_DuplicatePanics(t *testing.T) {
	resetForTest()
	Register("dup", nil, newFake("dup"))
	require.PanicsWithValue(t, `plugins.Register: duplicate plugin name "dup"`, func() {
		Register("dup", nil, newFake("dup"))
	})
}

func TestRegister_EmptyNamePanics(t *testing.T) {
	resetForTest()
	require.Panics(t, func() { Register("", nil, newFake("x")) })
}

func TestRegister_NilFactoryPanics(t *testing.T) {
	resetForTest()
	require.Panics(t, func() { Register("x", nil, nil) })
}

func TestBuild_PostInitAndProcessorOrder(t *testing.T) {
	resetForTest()
	Register("rule", []byte("name: rule\n"), newFakeProcessor("rule"))
	Register("snooze", []byte("name: snooze\n"), newFakeProcessor("snooze"))
	Register("notif", []byte("name: notif\n"), newFake("notif")) // non-Processor

	host := &nullHost{}
	all, procs, err := Build(context.Background(), host, []string{"rule", "snooze", "missing", "notif"})
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Contains(t, all, "rule")
	require.Contains(t, all, "snooze")
	require.Contains(t, all, "notif")

	// PostInit called once per plugin.
	for _, p := range all {
		switch v := p.(type) {
		case *fakeProcessor:
			require.Equal(t, 1, v.post, "post-init count for %s", v.Name())
		case *fakePlugin:
			require.Equal(t, 1, v.post)
		}
	}

	// Processors filtered by order; "missing" silently skipped, "notif"
	// included in `all` but not a Processor → not in procs.
	require.Len(t, procs, 2)
	require.Equal(t, "rule", procs[0].Name())
	require.Equal(t, "snooze", procs[1].Name())
}

func TestBuild_TwicePanics(t *testing.T) {
	resetForTest()
	Register("only", nil, newFake("only"))
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _, _ = Build(context.Background(), &nullHost{}, nil)
	})
}

func TestBuild_FactoryError(t *testing.T) {
	resetForTest()
	boom := errors.New("boom")
	Register("bad", []byte("name: bad\n"), func(meta Metadata) (Plugin, error) {
		return nil, boom
	})
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.ErrorIs(t, err, boom)
}

func TestBuild_NilPluginIsRejected(t *testing.T) {
	resetForTest()
	Register("nilp", nil, func(meta Metadata) (Plugin, error) { return nil, nil })
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.Error(t, err)
}

func TestBuild_MetadataParseError(t *testing.T) {
	resetForTest()
	Register("bad", []byte("not: : valid: yaml"), newFake("bad"))
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.Error(t, err)
}

func TestBuild_NameFallback(t *testing.T) {
	// Metadata yaml lacks `name:`; Build should fill it from the registry key.
	resetForTest()
	Register("auto", nil, func(meta Metadata) (Plugin, error) {
		require.Equal(t, "auto", meta.Name)
		return &fakePlugin{meta: meta}, nil
	})
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.NoError(t, err)
}
