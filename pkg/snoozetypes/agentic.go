package snoozetypes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// The agentic subtree is the AI-authored analysis of an alert: why it fired
// (root_cause) and what to do about it (remediation_plan), plus a
// server-stamped provenance block (analysis).
//
// It is deliberately small and closed. An agent writing it has a context
// window to spend on investigating, not on formatting; a downstream agent
// reading it wants fixed keys and closed enums it can branch on without
// re-parsing prose. Every field below earns its place by being something a
// consumer acts on: `confidence` decides whether to trust the conclusion,
// `risk` and `automatable` decide whether a fix can run unattended,
// `evidence` is what a human re-checks before approving, and `scope` says
// which thing to go look at.
//
// The subtree lives under the protected field `agentic` (see
// internal/protected): only the /api/v1/record/{uid}/agentic endpoint writes
// it, so these structs are the single definition of what a valid analysis is.

// Confidence levels accepted on RootCause.Confidence.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Risk levels accepted on Step.Risk.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Plan verdicts accepted on RemediationPlan.Status: what the alert needs from
// on-call right now, stated once instead of being inferred from step 1's prose.
const (
	PlanActionRequired = "action_required"
	PlanSelfResolved   = "self_resolved"
	PlanMonitoring     = "monitoring"
)

// Step timings accepted on Step.When: `now` is on-call work on this alert,
// `follow_up` is post-incident work that stops it recurring.
const (
	StepNow      = "now"
	StepFollowUp = "follow_up"
)

// Size limits, counted in CHARACTERS (runes), not bytes: these fields hold
// prose from a fleet whose hosts, paths and log lines are not all ASCII, and a
// limit that rejects a 380-character French summary for being "over 500" is a
// limit nobody can reason about.
//
// They exist to keep an alert document bounded — an agent that dumps a 4 MB
// journal into `evidence` would bloat every list query that returns the record.
const (
	MaxSummaryLen    = 500
	MaxDetailLen     = 2000
	MaxCaveats       = 5
	MaxCaveatLen     = 300
	MaxScopeLen      = 200
	MaxEvidenceItems = 10
	MaxEvidenceLen   = 500
	MaxSteps         = 20
	MaxActionLen     = 500
	MaxCommandLen    = 1000
	MaxSourceLen     = 64
)

// Rendered enum lists, reused verbatim in validation messages so a client
// reads the accepted values off the error instead of the docs.
const (
	confidenceEnum = "high|medium|low"
	riskEnum       = "low|medium|high"
	planStatusEnum = "action_required|self_resolved|monitoring"
	stepWhenEnum   = "now|follow_up"
)

// bodyPath addresses the body as a whole, for failures that belong to no
// single key (trailing data, a type error encoding/json could not attribute).
// It is the JSONPath root so a client can tell it apart from a real field.
const bodyPath = "$"

// nulMessage is one message, used at every string path: a caller that hit it
// twice should read the same sentence both times.
const nulMessage = "must not contain the NUL character"

// Agentic is the stored shape of the protected `agentic` field on a record.
type Agentic struct {
	RootCause       RootCause       `json:"root_cause"`
	RemediationPlan RemediationPlan `json:"remediation_plan"`
	Analysis        AnalysisMeta    `json:"analysis"`
}

// RootCause is the "why did this fire" half of the analysis.
type RootCause struct {
	// Summary is the one-sentence cause — the headline a triager reads. Required.
	Summary string `json:"summary"`
	// Detail is the longer explanation behind Summary: the chain of events,
	// timings, why other causes were ruled out. Optional.
	Detail string `json:"detail,omitempty"`
	// Scope names the thing that is broken, in whatever addressing scheme
	// fits the alert: "srv-victoria1:/var", "ovh/velero/kopia-maintain".
	// Optional but strongly encouraged — it is what a consumer greps on.
	Scope string `json:"scope,omitempty"`
	// Evidence holds the short observations the conclusion rests on: a
	// command and its telling line, a PromQL result. Optional.
	Evidence []string `json:"evidence,omitempty"`
	// Caveats are the limits of the investigation — what could not be
	// checked, what is inferred rather than observed. Kept apart from
	// Evidence so they are read before the conclusion is trusted. Optional.
	Caveats []string `json:"caveats,omitempty"`
	// Confidence is high|medium|low. Required. `low` is the honest answer
	// for an inconclusive investigation — record the trail rather than
	// leaving the alert to be re-investigated from scratch.
	Confidence string `json:"confidence"`
}

// RemediationPlan is the "what to do about it" half of the analysis.
type RemediationPlan struct {
	// Status is the verdict: action_required | self_resolved | monitoring.
	// Optional; absent means the plan does not say.
	Status string `json:"status,omitempty"`
	// Steps are the ordered fix actions. At least one is required.
	Steps []Step `json:"steps"`
	// Rollback is the ordered undo for Steps, in the same shape. Optional.
	Rollback []Step `json:"rollback,omitempty"`
	// Automatable states whether the steps are safe for an unattended agent
	// to execute. Optional; absent means false (needs a human).
	Automatable bool `json:"automatable,omitempty"`
}

// Step is one action in a plan.
type Step struct {
	// Action is what to do, in plain words. Required.
	Action string `json:"action"`
	// Command is the exact command implementing Action, when there is one.
	// Optional: some steps are "open an MR" or "ask the app team".
	Command string `json:"command,omitempty"`
	// Risk is low|medium|high — the blast radius of running this step.
	// Required, because an executor gates on it.
	Risk string `json:"risk"`
	// When is now | follow_up: whether on-call runs this step while the
	// alert is live, or it is post-incident work. Optional.
	When string `json:"when,omitempty"`
}

// AnalysisMeta is provenance, stamped by the server. Clients cannot set it:
// a forged `by` or a backdated `at` would make the audit trail useless.
type AnalysisMeta struct {
	// At is when the analysis was stored, RFC3339 in UTC.
	At string `json:"at"`
	// By is the authenticated subject that stored it.
	By string `json:"by"`
	// Source is the caller-supplied tool tag ("alert-rca", "snooze-cli").
	Source string `json:"source,omitempty"`
}

// AgenticRequest is the wire body of PUT /api/v1/record/{uid}/agentic. It
// mirrors Agentic minus the server-stamped provenance, plus the Source tag
// that feeds into it.
type AgenticRequest struct {
	RootCause       RootCause       `json:"root_cause"`
	RemediationPlan RemediationPlan `json:"remediation_plan"`
	// Source is an optional short tag naming the tool that produced the
	// analysis. It lands in analysis.source.
	Source string `json:"source,omitempty"`
}

// FieldError is one validation failure, addressed by JSON path so the caller
// can point at the offending key without guessing.
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationErrors is the set of failures from one validation pass.
type ValidationErrors []FieldError

// Error renders the failures as a single line, most useful in logs and CLI
// output.
func (v ValidationErrors) Error() string {
	parts := make([]string, 0, len(v))
	for _, fe := range v {
		parts = append(parts, fe.Path+": "+fe.Message)
	}
	return strings.Join(parts, "; ")
}

// Details renders the failures as the map the API error envelope carries, so
// a client gets {"root_cause.confidence": "must be one of high|medium|low"}
// instead of having to parse a sentence.
func (v ValidationErrors) Details() map[string]any {
	out := make(map[string]any, len(v))
	for _, fe := range v {
		out[fe.Path] = fe.Message
	}
	return out
}

// Validate checks the request against the schema and returns every failure
// found, not just the first — an agent fixing its payload should need one
// round-trip, not five.
func (req *AgenticRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	errs = append(errs, validateRootCause(&req.RootCause)...)
	errs = append(errs, validateRemediationPlan(&req.RemediationPlan)...)
	if utf8.RuneCountInString(req.Source) > MaxSourceLen {
		errs = append(errs, FieldError{"source", fmt.Sprintf("must be at most %d characters", MaxSourceLen)})
	}
	errs = append(errs, checkNUL("source", req.Source)...)
	return errs
}

// checkNUL rejects a NUL byte anywhere in a string. PostgreSQL jsonb cannot
// hold \u0000 — it refuses the escape outright — so a payload carrying one
// validates here and then fails at the write, turning a caller mistake into a
// 500 on one backend and a silent success on the other two.
func checkNUL(path, s string) ValidationErrors {
	if strings.ContainsRune(s, 0) {
		return ValidationErrors{{path, nulMessage}}
	}
	return nil
}

func validateRootCause(rc *RootCause) ValidationErrors {
	var errs ValidationErrors
	switch {
	case strings.TrimSpace(rc.Summary) == "":
		errs = append(errs, FieldError{"root_cause.summary", "is required"})
	case utf8.RuneCountInString(rc.Summary) > MaxSummaryLen:
		errs = append(errs, FieldError{"root_cause.summary", fmt.Sprintf("must be at most %d characters", MaxSummaryLen)})
	}
	errs = append(errs, checkNUL("root_cause.summary", rc.Summary)...)
	if utf8.RuneCountInString(rc.Detail) > MaxDetailLen {
		errs = append(errs, FieldError{"root_cause.detail", fmt.Sprintf("must be at most %d characters", MaxDetailLen)})
	}
	errs = append(errs, checkNUL("root_cause.detail", rc.Detail)...)
	if utf8.RuneCountInString(rc.Scope) > MaxScopeLen {
		errs = append(errs, FieldError{"root_cause.scope", fmt.Sprintf("must be at most %d characters", MaxScopeLen)})
	}
	errs = append(errs, checkNUL("root_cause.scope", rc.Scope)...)
	switch rc.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	case "":
		errs = append(errs, FieldError{"root_cause.confidence", "is required (" + confidenceEnum + ")"})
	default:
		errs = append(errs, FieldError{"root_cause.confidence", "must be one of " + confidenceEnum})
	}
	if len(rc.Evidence) > MaxEvidenceItems {
		errs = append(errs, FieldError{"root_cause.evidence", fmt.Sprintf("must hold at most %d items", MaxEvidenceItems)})
	}
	for i, ev := range rc.Evidence {
		path := fmt.Sprintf("root_cause.evidence[%d]", i)
		if strings.TrimSpace(ev) == "" {
			errs = append(errs, FieldError{path, "must not be empty"})
			continue
		}
		if utf8.RuneCountInString(ev) > MaxEvidenceLen {
			errs = append(errs, FieldError{path, fmt.Sprintf("must be at most %d characters", MaxEvidenceLen)})
		}
		errs = append(errs, checkNUL(path, ev)...)
	}
	if len(rc.Caveats) > MaxCaveats {
		errs = append(errs, FieldError{"root_cause.caveats", fmt.Sprintf("must hold at most %d items", MaxCaveats)})
	}
	for i, c := range rc.Caveats {
		path := fmt.Sprintf("root_cause.caveats[%d]", i)
		if strings.TrimSpace(c) == "" {
			errs = append(errs, FieldError{path, "must not be empty"})
			continue
		}
		if utf8.RuneCountInString(c) > MaxCaveatLen {
			errs = append(errs, FieldError{path, fmt.Sprintf("must be at most %d characters", MaxCaveatLen)})
		}
		errs = append(errs, checkNUL(path, c)...)
	}
	return errs
}

func validateRemediationPlan(rp *RemediationPlan) ValidationErrors {
	var errs ValidationErrors
	switch rp.Status {
	case "", PlanActionRequired, PlanSelfResolved, PlanMonitoring:
	default:
		errs = append(errs, FieldError{"remediation_plan.status", "must be one of " + planStatusEnum})
	}
	if len(rp.Steps) == 0 {
		errs = append(errs, FieldError{"remediation_plan.steps", "must hold at least one step"})
	}
	errs = append(errs, validateSteps("remediation_plan.steps", rp.Steps)...)
	errs = append(errs, validateSteps("remediation_plan.rollback", rp.Rollback)...)
	return errs
}

func validateSteps(prefix string, steps []Step) ValidationErrors {
	var errs ValidationErrors
	if len(steps) > MaxSteps {
		return append(errs, FieldError{prefix, fmt.Sprintf("must hold at most %d steps", MaxSteps)})
	}
	for i := range steps {
		path := fmt.Sprintf("%s[%d]", prefix, i)
		s := &steps[i]
		switch {
		case strings.TrimSpace(s.Action) == "":
			errs = append(errs, FieldError{path + ".action", "is required"})
		case utf8.RuneCountInString(s.Action) > MaxActionLen:
			errs = append(errs, FieldError{path + ".action", fmt.Sprintf("must be at most %d characters", MaxActionLen)})
		}
		errs = append(errs, checkNUL(path+".action", s.Action)...)
		if utf8.RuneCountInString(s.Command) > MaxCommandLen {
			errs = append(errs, FieldError{path + ".command", fmt.Sprintf("must be at most %d characters", MaxCommandLen)})
		}
		errs = append(errs, checkNUL(path+".command", s.Command)...)
		switch s.Risk {
		case RiskLow, RiskMedium, RiskHigh:
		case "":
			errs = append(errs, FieldError{path + ".risk", "is required (" + riskEnum + ")"})
		default:
			errs = append(errs, FieldError{path + ".risk", "must be one of " + riskEnum})
		}
		switch s.When {
		case "", StepNow, StepFollowUp:
		default:
			errs = append(errs, FieldError{path + ".when", "must be one of " + stepWhenEnum})
		}
	}
	return errs
}

// DecodeAgenticRequest parses a request body strictly: an unknown key is an
// error rather than something silently dropped. A typo'd `remediation` or a
// hopeful `analysis` (which clients may not set) must fail loudly, or the
// agent that sent it will believe it stored something it did not.
//
// The returned error is a ValidationErrors when the body parsed but broke the
// schema — including a value of the wrong JSON type, which is a fixable
// mistake at a known path and not a reason to answer "malformed body" — and a
// plain error only when the bytes are not decodable JSON at all.
func DecodeAgenticRequest(body []byte) (AgenticRequest, error) {
	var req AgenticRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if path, ok := unknownFieldPath(err); ok {
			return req, ValidationErrors{{path, "unknown field"}}
		}
		if fe, ok := typeMismatch(err); ok {
			return req, ValidationErrors{fe}
		}
		return req, err
	}
	// json.Decoder stops at the end of the first value, so a body holding a
	// second object — or a concatenation accident — would store the first and
	// report success. Anything but whitespace after it is a caller bug.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return req, ValidationErrors{{bodyPath, "unexpected data after the JSON object"}}
	}
	if errs := req.Validate(); len(errs) > 0 {
		return req, errs
	}
	return req, nil
}

// typeMismatch turns encoding/json's type error into a field error. Its Field
// already carries the dotted path; its Type and Value are Go-flavoured
// ("[]snoozetypes.Step"), so both are rendered as the JSON kinds the caller
// actually wrote.
func typeMismatch(err error) (FieldError, bool) {
	var terr *json.UnmarshalTypeError
	if !errors.As(err, &terr) {
		return FieldError{}, false
	}
	path := terr.Field
	if path == "" {
		path = bodyPath
	}
	return FieldError{path, "must be " + jsonKind(terr.Type) + ", got " + terr.Value}, true
}

// jsonKind names a Go type the way a JSON author thinks of it.
func jsonKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Struct, reflect.Map:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return t.String()
	}
}

// unknownFieldPath extracts the key name from encoding/json's
// `json: unknown field "foo"` error, which carries no typed form.
func unknownFieldPath(err error) (string, bool) {
	const prefix = "json: unknown field "
	msg := err.Error()
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return "", false
	}
	name := strings.Trim(msg[idx+len(prefix):], `"`)
	if name == "" {
		return "", false
	}
	return name, true
}

// ToAgentic converts a validated request into the stored subtree, stamping
// provenance. Callers pass the authenticated subject and the wall clock; the
// timestamp is normalised to UTC RFC3339 so every record sorts and compares
// the same way regardless of server locale.
func (req *AgenticRequest) ToAgentic(by string, now time.Time) Agentic {
	return Agentic{
		RootCause:       req.RootCause,
		RemediationPlan: req.RemediationPlan,
		Analysis: AnalysisMeta{
			At:     now.UTC().Format(time.RFC3339),
			By:     by,
			Source: req.Source,
		},
	}
}

// Document renders the subtree as the loose map the db layer stores, going
// through JSON so the wire shape and the stored shape cannot drift apart.
func (a Agentic) Document() (map[string]any, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}
