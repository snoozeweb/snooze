package k8sevents

import (
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Event is the subset of the core/v1 Event we care about. The apiserver emits
// camelCase JSON. Fields we don't map (action, related, series, etc.) are
// ignored by the decoder.
type Event struct {
	Metadata            objectMeta      `json:"metadata"`
	InvolvedObject      objectReference `json:"involvedObject"`
	Reason              string          `json:"reason"`
	Message             string          `json:"message"`
	Type                string          `json:"type"` // "Normal" | "Warning"
	LastTimestamp       time.Time       `json:"lastTimestamp"`
	FirstTimestamp      time.Time       `json:"firstTimestamp"`
	EventTime           time.Time       `json:"eventTime"`
	Count               int             `json:"count"`
	Source              eventSource     `json:"source"`
	ReportingController string          `json:"reportingComponent"`
	ReportingInstance   string          `json:"reportingInstance"`
}

type objectMeta struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	ResourceVersion string `json:"resourceVersion"`
	UID             string `json:"uid"`
}

type objectReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

type eventSource struct {
	Component string `json:"component"`
	Host      string `json:"host"`
}

// status is the apiserver's error envelope, streamed as the object body when a
// watch fails (e.g. a 410 Gone arrives as {"type":"ERROR","object":{...Status...}}).
type status struct {
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason"` // "Expired" / "Gone" on a compacted resourceVersion
	Code    int    `json:"code"`
}

// dedupKey is the value the watcher de-duplicates on: the involved object plus
// the reason. Repeated occurrences of the same key inside Config.DedupWindow
// are suppressed (Kubernetes itself coalesces with .count, but a watch still
// streams a MODIFIED envelope per bump).
func (e Event) dedupKey() string {
	io := e.InvolvedObject
	return strings.Join([]string{io.Namespace, io.Kind, io.Name, e.Reason}, "/")
}

// occurredAt returns the best timestamp for the event, preferring the most
// specific/recent field available and falling back to now.
func (e Event) occurredAt() time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp
	case !e.EventTime.IsZero():
		return e.EventTime
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp
	default:
		return time.Now().UTC()
	}
}

// severityFor resolves the Snooze severity for the event: a reason-specific
// override (from the merged Config.Reasons map) wins; otherwise the
// type-derived default applies (Warning→warning, Normal→info).
func (c Config) severityFor(e Event) string {
	if sev, ok := c.Reasons[e.Reason]; ok && sev != "" {
		return sev
	}
	if e.Type == "Normal" {
		return "info"
	}
	return "warning"
}

// ToRecord maps a Kubernetes Event into the canonical snoozetypes.Record.
// Exposed so tests exercise the mapping without any HTTP. The mapping mirrors
// the spec:
//
//   - Source   "kubernetes"
//   - Host     involvedObject.name, falling back to source.host
//   - Process  "<involvedObject.kind>/<reason>"
//   - Severity reason override → type default
//   - Message  event.message
//   - Environment  involvedObject.namespace (fallback metadata.namespace)
//   - Raw      namespace / reason / count / involvedObject / source
func (c Config) ToRecord(e Event) snoozetypes.Record {
	host := e.InvolvedObject.Name
	if host == "" {
		host = e.Source.Host
	}

	process := e.Reason
	if e.InvolvedObject.Kind != "" {
		if e.Reason != "" {
			process = e.InvolvedObject.Kind + "/" + e.Reason
		} else {
			process = e.InvolvedObject.Kind
		}
	}

	namespace := e.InvolvedObject.Namespace
	if namespace == "" {
		namespace = e.Metadata.Namespace
	}

	raw := map[string]any{
		"namespace": namespace,
		"reason":    e.Reason,
		"count":     e.Count,
		"type":      e.Type,
		"involved_object": map[string]any{
			"kind":      e.InvolvedObject.Kind,
			"name":      e.InvolvedObject.Name,
			"namespace": e.InvolvedObject.Namespace,
			"uid":       e.InvolvedObject.UID,
		},
		"source": map[string]any{
			"component": e.Source.Component,
			"host":      e.Source.Host,
		},
		"event_name":       e.Metadata.Name,
		"resource_version": e.Metadata.ResourceVersion,
	}
	if e.ReportingController != "" {
		raw["reporting_component"] = e.ReportingController
	}

	return snoozetypes.Record{
		Host:        host,
		Source:      "kubernetes",
		Process:     process,
		Severity:    c.severityFor(e),
		Message:     e.Message,
		Timestamp:   e.occurredAt(),
		Environment: namespace,
		Raw:         raw,
	}
}
