package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// samlStateCookie is the name of the short-lived signed cookie that carries the
// CSRF RelayState (plus return-to / org) across the IdP round-trip. It mirrors
// the OIDC state cookie but uses a distinct name + HMAC key label.
const samlStateCookie = "snooze_saml_relaystate"

// samlStateLabel is the TokenEngine.DeriveKey label for the cookie's HMAC key.
const samlStateLabel = "saml-relaystate-v1"

// samlState is the payload signed into the RelayState cookie. The IdP only
// echoes the opaque State token back in the form RelayState; the full payload
// (return-to, org, expiry) rides in the cookie. JSON keys are short to keep the
// cookie small.
type samlState struct {
	State    string `json:"s"`
	ReturnTo string `json:"r,omitempty"`
	Org      string `json:"o,omitempty"`
	Exp      int64  `json:"e"`
}

// encodeSAMLState serialises st and appends an HMAC-SHA256 tag:
// base64url(json) "." base64url(mac).
func encodeSAMLState(key []byte, st samlState) string {
	payload, err := json.Marshal(st)
	if err != nil {
		// Unreachable for samlState (all strings + int64); guard so a future
		// field change can never silently emit an unsigned cookie. An empty
		// value fails closed at decodeSAMLState (malformed → login restart).
		return ""
	}
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(b64))
	tag := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return b64 + "." + tag
}

// decodeSAMLState verifies the tag (constant time) and expiry, then returns st.
func decodeSAMLState(key []byte, raw string) (samlState, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return samlState{}, errors.New("saml state: malformed cookie")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[1])) != 1 {
		return samlState{}, errors.New("saml state: bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return samlState{}, errors.New("saml state: bad payload")
	}
	var st samlState
	if err := json.Unmarshal(payload, &st); err != nil {
		return samlState{}, errors.New("saml state: bad json")
	}
	if st.Exp <= time.Now().Unix() {
		return samlState{}, errors.New("saml state: expired")
	}
	return st, nil
}
