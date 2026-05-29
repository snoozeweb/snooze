package teams

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrNoToken is returned by tokenStore.Load when the cache file does not
// exist. The daemon surfaces this at startup with a hint that the operator
// must run `snooze-teams authorize` once.
var ErrNoToken = errors.New("teams: no cached OAuth token; run `snooze-teams authorize`")

// cachedToken is the persistent representation of a delegated OAuth2 grant.
// The shape is intentionally narrow: we only store what's needed to mint a
// new access token from the refresh token, plus the access token itself so
// short daemon restarts don't burn a refresh on every reload.
//
// The file is rewritten on every successful token refresh, since AAD may
// return a fresh refresh_token alongside the access_token (refresh-token
// rotation is enabled by default for confidential clients).
type cachedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
	ObtainedAt   time.Time `json:"obtained_at"`
	TenantID     string    `json:"tenant_id,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
}

// tokenStore is a tiny file-backed cache for the delegated OAuth2 grant.
// Concurrency: the in-memory cache is protected by mu; the on-disk write is
// atomic (temp-file + rename) so a crash mid-write cannot truncate the
// previous token. The store does not poll the disk — it reflects what was
// last loaded or saved by this process.
type tokenStore struct {
	path string

	mu     sync.Mutex
	loaded *cachedToken // nil until first Load
}

// newTokenStore constructs a store backed by path. Path is resolved lazily on
// the first Load/Save so that callers can construct stores during config
// parsing without forcing the file to exist yet.
func newTokenStore(path string) *tokenStore {
	return &tokenStore{path: path}
}

// Load reads the cache file from disk. Returns ErrNoToken (wrapped) when the
// file is absent so callers can distinguish "fresh install" from "corrupt
// cache". Calls after the first one return the in-memory copy.
func (s *tokenStore) Load() (cachedToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded != nil {
		return *s.loaded, nil
	}
	raw, err := os.ReadFile(s.path) //nolint:gosec // operator-supplied path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cachedToken{}, fmt.Errorf("%w (path: %s)", ErrNoToken, s.path)
		}
		return cachedToken{}, fmt.Errorf("teams: read token cache %q: %w", s.path, err)
	}
	var t cachedToken
	if err := json.Unmarshal(raw, &t); err != nil {
		return cachedToken{}, fmt.Errorf("teams: parse token cache %q: %w", s.path, err)
	}
	if t.RefreshToken == "" {
		return cachedToken{}, fmt.Errorf("teams: token cache %q is missing refresh_token", s.path)
	}
	s.loaded = &t
	return t, nil
}

// Save persists tok to disk atomically. The write goes to "<path>.tmp" first
// and is then renamed; either the old or the new file is visible at any
// moment, never a half-written file. The in-memory copy is updated last so a
// failed disk write leaves the previous Load result in place.
func (s *tokenStore) Save(tok cachedToken) error {
	if tok.RefreshToken == "" {
		return errors.New("teams: refusing to save token without refresh_token")
	}
	raw, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // G117: persisting the OAuth token is this store's purpose; it is written to a 0o600 file in a 0o700 dir
	if err != nil {
		return fmt.Errorf("teams: encode token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("teams: mkdir token dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("teams: write token tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("teams: rename token cache: %w", err)
	}
	s.mu.Lock()
	cp := tok
	s.loaded = &cp
	s.mu.Unlock()
	return nil
}

// Path returns the on-disk path the store is bound to. Useful for log
// messages — never use this to bypass Load/Save.
func (s *tokenStore) Path() string { return s.path }
