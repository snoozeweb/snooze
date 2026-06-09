package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// oidcStateCookie is the name of the short-lived signed cookie that carries the
// CSRF state, OIDC nonce and PKCE verifier across the IdP round-trip.
const oidcStateCookie = "snooze_oidc_state"

// oidcStateLabel is the TokenEngine.DeriveKey label for the cookie's HMAC key.
const oidcStateLabel = "oidc-state-v1"

// oidcState is the payload signed into the state cookie. JSON keys are short to
// keep the cookie small.
type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
	Org      string `json:"o,omitempty"`
	Exp      int64  `json:"e"`
}

// encodeOIDCState serialises st and appends an HMAC-SHA256 tag:
// base64url(json) "." base64url(mac).
func encodeOIDCState(key []byte, st oidcState) string {
	payload, err := json.Marshal(st)
	if err != nil {
		// Unreachable for oidcState (all strings + int64); guard so a future
		// field change can never silently emit an unsigned cookie. An empty
		// value fails closed at decodeOIDCState (malformed → login restart).
		return ""
	}
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(b64))
	tag := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return b64 + "." + tag
}

// decodeOIDCState verifies the tag (constant time) and expiry, then returns st.
func decodeOIDCState(key []byte, raw string) (oidcState, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return oidcState{}, errors.New("oidc state: malformed cookie")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[1])) != 1 {
		return oidcState{}, errors.New("oidc state: bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oidcState{}, errors.New("oidc state: bad payload")
	}
	var st oidcState
	if err := json.Unmarshal(payload, &st); err != nil {
		return oidcState{}, errors.New("oidc state: bad json")
	}
	if st.Exp <= time.Now().Unix() {
		return oidcState{}, errors.New("oidc state: expired")
	}
	return st, nil
}

// randURLToken returns an n-byte cryptographically-random base64url token.
func randURLToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
