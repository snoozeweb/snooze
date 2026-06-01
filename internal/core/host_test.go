package core

import (
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/stretchr/testify/require"
)

func TestCoreExposesAsyncWriter(t *testing.T) {
	c := &Core{Async: asyncwriter.New(nil, time.Second, nil)}
	require.NotNil(t, c.AsyncWriter(), "Core must expose the async writer it builds at boot")
	require.Same(t, c.Async, c.AsyncWriter())
}
