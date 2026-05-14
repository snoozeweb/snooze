package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
)

// secretsCollection is the storage collection holding the JWT private key and
// the cluster reload token. The Python port uses the same name.
const secretsCollection = "secrets"

// SecretJWTPrivateKey is the canonical secret name for the HS256 signing key.
const SecretJWTPrivateKey = "jwt_private_key"

// SecretReloadToken is the canonical secret name for the cross-node reload
// token.
const SecretReloadToken = "reload_token"

// JWTKeyBytes is the raw byte length of a freshly generated JWT signing key.
// 64 bytes is twice the SHA-256 block size — comfortably above the
// auth.MinSecretBytes (32) requirement.
const JWTKeyBytes = 64

// ReloadTokenBytes is the raw byte length of a freshly generated reload token.
const ReloadTokenBytes = 32

// EnsureSecrets idempotently fetches (and on first boot generates) the JWT
// signing key and the cluster reload token. The values are stored in the
// ``secrets`` collection under ``{type: "secret", name: <name>, value: <b64>}``
// rows that the Python codebase already populates.
//
// Returns:
//   - jwtKey: raw bytes ready to feed into auth.NewTokenEngine.
//   - reloadToken: base64url string used by the syncer.
func EnsureSecrets(ctx context.Context, drv db.Driver) (jwtKey []byte, reloadToken string, err error) {
	if drv == nil {
		return nil, "", errors.New("ensure_secrets: nil db driver")
	}

	existing, err := readSecrets(ctx, drv)
	if err != nil {
		return nil, "", fmt.Errorf("ensure_secrets: read: %w", err)
	}

	var towrite []db.Document

	jwtVal, hasJWT := existing[SecretJWTPrivateKey]
	if !hasJWT {
		raw := make([]byte, JWTKeyBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, "", fmt.Errorf("ensure_secrets: jwt rand: %w", err)
		}
		jwtVal = base64.RawURLEncoding.EncodeToString(raw)
		towrite = append(towrite, db.Document{
			"type":  "secret",
			"name":  SecretJWTPrivateKey,
			"value": jwtVal,
		})
	}

	reloadVal, hasReload := existing[SecretReloadToken]
	if !hasReload {
		raw := make([]byte, ReloadTokenBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, "", fmt.Errorf("ensure_secrets: reload rand: %w", err)
		}
		reloadVal = base64.RawURLEncoding.EncodeToString(raw)
		towrite = append(towrite, db.Document{
			"type":  "secret",
			"name":  SecretReloadToken,
			"value": reloadVal,
		})
	}

	if len(towrite) > 0 {
		if _, err := drv.Write(ctx, secretsCollection, towrite, db.WriteOptions{
			Primary:    []string{"type", "name"},
			UpdateTime: true,
		}); err != nil {
			return nil, "", fmt.Errorf("ensure_secrets: write: %w", err)
		}
	}

	jwtBytes, err := base64.RawURLEncoding.DecodeString(jwtVal)
	if err != nil {
		// Tolerate older Python rows that stored raw token_urlsafe strings:
		// fall back to using the bytes of the string itself.
		jwtBytes = []byte(jwtVal)
	}
	return jwtBytes, reloadVal, nil
}

// readSecrets returns every {name → value} pair currently stored in the
// secrets collection. Missing collection is not an error.
func readSecrets(ctx context.Context, drv db.Driver) (map[string]string, error) {
	docs, _, err := drv.Search(ctx, secretsCollection, condition.Cond{}, db.Page{})
	if err != nil {
		// Treat the collection-missing case as empty.
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(docs))
	for _, d := range docs {
		name, _ := d["name"].(string)
		val, _ := d["value"].(string)
		if name != "" {
			out[name] = val
		}
	}
	return out, nil
}
