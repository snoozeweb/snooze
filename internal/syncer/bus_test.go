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

func TestTopicMatches(t *testing.T) {
	cases := []struct {
		name   string
		topic  string
		prefix string
		want   bool
	}{
		{"exact", "collection.notification", "collection.notification", true},
		{"tenant suffix", "collection.notification.acme", "collection.notification", true},
		{"empty tenant is the bare topic", CollectionTopic("notification", ""), "collection.notification", true},
		{"tenant slug containing dots", "collection.notification.a.b", "collection.notification", true},
		// The regression this helper exists for: `notificationlog` must not
		// feed the `notification` plugin's subscription.
		{"longer sibling collection", "collection.notificationlog.default", "collection.notification", false},
		{"longer sibling, no tenant", "collection.notificationlog", "collection.notification", false},
		{"reverse direction", "collection.notification.acme", "collection.notificationlog", false},
		{"unrelated collection", "collection.rule.acme", "collection.notification", false},
		{"plugin topic vs collection prefix", "plugin.notification", "collection.notification", false},
		{"plugin topic exact", "plugin.notification", "plugin.notification", true},
		{"plugin sibling", "plugin.notificationlog", "plugin.notification", false},
		{"empty prefix is the wildcard", "collection.anything.acme", "", true},
		{"empty prefix matches empty topic", "", "", true},
		{"empty topic never matches a real prefix", "", "collection.notification", false},
		{"prefix ending in a dot still works", "collection.notification.acme", "collection.notification.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, TopicMatches(tc.topic, tc.prefix))
		})
	}
}
