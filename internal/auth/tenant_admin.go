package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/db"
)

// DefaultAdminUsername is the name of a tenant's first local admin when the
// caller does not specify one.
const DefaultAdminUsername = "admin"

// TenantAdminResult reports the outcome of EnsureTenantAdmin. Password is the
// freshly generated plaintext — return it to the operator exactly once. It is
// empty when an existing admin was left untouched (resetIfExists=false).
type TenantAdminResult struct {
	Username string
	Password string
	Created  bool
}

// EnsureTenantAdmin provisions (or resets) a local admin user. ctx MUST be
// scoped to the target tenant via auth.WithTenant and MUST NOT carry platform
// scope, so the driver stamps tenant_id on the user. username defaults to
// DefaultAdminUsername.
//
//   - user absent  → create it with role ["admin"] and a generated password.
//   - user present → if resetIfExists, regenerate its password (other fields,
//     incl. roles, are preserved by the primary-key merge); otherwise leave it
//     untouched and return Password=="" / Created==false.
//
// The user document is written directly (not via the user plugin), so the
// bcrypt hash is computed here — mirroring EnsureRoot.
func EnsureTenantAdmin(ctx context.Context, driver db.Driver, username string, resetIfExists bool) (TenantAdminResult, error) {
	if driver == nil {
		return TenantAdminResult{}, fmt.Errorf("auth: nil db driver")
	}
	if username == "" {
		username = DefaultAdminUsername
	}

	// A miss is db.ErrNotFound (the contract for every real driver). Any OTHER
	// error is a genuine failure and must propagate — silently treating it as
	// "absent" could fall into the create branch during a reset and clobber an
	// existing admin's roles.
	existing, err := driver.GetOne(ctx, LocalCollection, db.Document{
		"name":   username,
		"method": LocalMethod,
	})
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return TenantAdminResult{}, fmt.Errorf("auth: lookup tenant admin: %w", err)
	}
	exists := existing != nil
	if exists && !resetIfExists {
		return TenantAdminResult{Username: username, Created: false}, nil
	}

	password, err := generatePassword(BootstrapPasswordBytes)
	if err != nil {
		return TenantAdminResult{}, fmt.Errorf("auth: generate password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return TenantAdminResult{}, fmt.Errorf("auth: hash password: %w", err)
	}

	var doc db.Document
	if exists {
		// Reset: merge-update the password only; the [name,method] primary key
		// match preserves roles/groups/enabled.
		doc = db.Document{"name": username, "method": LocalMethod, "password": string(hash)}
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		doc = db.Document{
			"name":       username,
			"method":     LocalMethod,
			"enabled":    true,
			"password":   string(hash),
			"roles":      []string{"admin"},
			"groups":     []string{},
			"created_at": now,
		}
	}
	if _, err := driver.Write(ctx, LocalCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"name", "method"},
		UpdateTime: true,
	}); err != nil {
		return TenantAdminResult{}, fmt.Errorf("auth: write tenant admin: %w", err)
	}
	return TenantAdminResult{Username: username, Password: password, Created: !exists}, nil
}
