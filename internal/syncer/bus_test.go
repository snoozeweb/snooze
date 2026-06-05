// internal/syncer/bus_test.go
package syncer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectionTopic_Scoped(t *testing.T) {
	require.Equal(t, "collection.rule.acme", CollectionTopic("rule", "acme"))
}

func TestCollectionTopic_Global(t *testing.T) {
	// Global collections (empty tenant) produce the legacy un-suffixed topic.
	require.Equal(t, "collection.tenant", CollectionTopic("tenant", ""))
}

func TestEvent_TenantField(t *testing.T) {
	e := Event{
		Topic:      CollectionTopic("rule", "acme"),
		Op:         "write",
		Collection: "rule",
		Tenant:     "acme",
	}
	require.Equal(t, "acme", e.Tenant)
	require.Equal(t, "collection.rule.acme", e.Topic)
}
