package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/japannext/snooze/internal/db"
)

// LocalCollection is the collection holding local user documents. The
// password (bcrypt-hashed) lives on the user document itself; the Python
// codebase split it across `user` + `user.password`, but the Go port collapses
// the two for simplicity. Migration scripts will rewrite legacy installs.
const LocalCollection = "user"

// LocalMethod is the auth method string set on Identity.
const LocalMethod = "local"

// LocalProvider authenticates against bcrypt-hashed passwords in the user
// collection of the configured db.Driver.
type LocalProvider struct {
	DB db.Driver
}

// NewLocalProvider returns a ready-to-use local provider backed by the given
// driver.
func NewLocalProvider(driver db.Driver) *LocalProvider { return &LocalProvider{DB: driver} }

// Name returns the provider identifier.
func (l *LocalProvider) Name() string { return LocalMethod }

// Authenticate fetches the user document, compares the bcrypt hash, and
// returns the resulting Identity. It returns ErrInvalidCredentials for every
// unhappy path that involves the username/password pair (missing user, wrong
// password, malformed hash) so that callers cannot distinguish.
func (l *LocalProvider) Authenticate(ctx context.Context, c Credentials) (Identity, error) {
	if l.DB == nil {
		return Identity{}, errors.New("local provider: nil db driver")
	}
	if c.Username == "" || c.Password == "" {
		return Identity{}, ErrInvalidCredentials
	}

	user, err := l.DB.GetOne(ctx, LocalCollection, db.Document{
		"name":   c.Username,
		"method": LocalMethod,
	})
	if err != nil {
		// Treat any lookup miss as a credentials failure to avoid user-enumeration.
		return Identity{}, fmt.Errorf("local auth lookup: %w", ErrInvalidCredentials)
	}

	if enabled, ok := user["enabled"].(bool); ok && !enabled {
		return Identity{}, ErrUserDisabled
	}

	hash, _ := user["password"].(string)
	if hash == "" {
		// Constant-time dummy compare to keep timing flat between
		// missing-hash and bad-password paths.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalidi"), []byte(c.Password))
		return Identity{}, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.Password)); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	id := Identity{
		Username: c.Username,
		Method:   LocalMethod,
		Groups:   stringSliceField(user, "groups"),
	}
	// Constant-time username compare against the doc to guard against any
	// accidental drift between the lookup filter and the stored doc.
	if name, _ := user["name"].(string); subtle.ConstantTimeCompare([]byte(name), []byte(c.Username)) != 1 {
		return Identity{}, ErrInvalidCredentials
	}
	return id, nil
}

// stringSliceField pulls a []string out of a free-form Document field,
// tolerating both `[]string` and `[]any` shapes produced by JSON decoders.
func stringSliceField(doc db.Document, field string) []string {
	if doc == nil {
		return nil
	}
	switch v := doc[field].(type) {
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
