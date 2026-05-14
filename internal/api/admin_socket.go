package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/internal/auth"
)

// AdminServer is a minimal HTTP server bound to a unix socket. It exposes a
// single privileged endpoint (/api/root_token) without going through the
// regular auth middleware; the socket's filesystem permissions plus the
// SO_PEERCRED check (Linux build) are the only access controls.
type AdminServer struct {
	// Path is the socket file path. Created on Listen, removed on Shutdown.
	Path string
	// Tokens is the JWT engine used to mint the root token.
	Tokens *auth.TokenEngine
	// UID is the uid the snooze daemon runs as. The socket is chowned to
	// this uid when the process can do so (typically root).
	UID int
	// AllowedUIDs is the (additional) set of peer uids permitted to read
	// the root token. Zero (root) and UID are always permitted; this slice
	// is appended on top of those.
	AllowedUIDs []int
	// Logger receives admin-socket lifecycle events. Never logs the token.
	Logger *slog.Logger
	// GracePeriod caps Shutdown. Zero defaults to 5s.
	GracePeriod time.Duration

	server *http.Server
}

// ListenAndServe binds the unix socket and serves /api/root_token until ctx
// is cancelled. Returns nil on a clean shutdown.
func (a *AdminServer) ListenAndServe(ctx context.Context) error {
	if a.Path == "" {
		return errors.New("admin socket: empty path")
	}
	if a.Tokens == nil {
		return errors.New("admin socket: nil token engine")
	}

	if err := removeSocket(a.Path); err != nil {
		return fmt.Errorf("admin socket: cleanup: %w", err)
	}
	ln, err := net.Listen("unix", a.Path)
	if err != nil {
		return fmt.Errorf("admin socket: listen: %w", err)
	}
	// Tighten perms before we accept anything.
	if err := os.Chmod(a.Path, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("admin socket: chmod: %w", err)
	}
	if os.Geteuid() == 0 && a.UID > 0 {
		if err := os.Chown(a.Path, a.UID, -1); err != nil {
			_ = ln.Close()
			return fmt.Errorf("admin socket: chown: %w", err)
		}
	}

	// Wrap with a peer-cred check. Linux build provides SO_PEERCRED; non-Linux
	// builds reject every connection.
	wrapped := &peerCredListener{Listener: ln, allowed: a.allowedSet()}

	r := chi.NewRouter()
	r.Get("/api/root_token", a.handleRootToken)

	srv := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	a.server = srv

	if a.Logger != nil {
		a.Logger.Info("admin socket listening", slog.String("path", a.Path))
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(wrapped) }()

	grace := a.GracePeriod
	if grace <= 0 {
		grace = 5 * time.Second
	}

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		err := srv.Shutdown(shutCtx)
		_ = os.Remove(a.Path)
		if err != nil {
			return fmt.Errorf("admin socket: shutdown: %w", err)
		}
		if e := <-errCh; e != nil && !errors.Is(e, http.ErrServerClosed) {
			return fmt.Errorf("admin socket: serve: %w", e)
		}
		return nil
	case err := <-errCh:
		_ = os.Remove(a.Path)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("admin socket: serve: %w", err)
		}
		return nil
	}
}

// allowedSet returns the canonical set of allowed peer uids.
func (a *AdminServer) allowedSet() map[int]struct{} {
	out := map[int]struct{}{0: {}}
	if a.UID > 0 {
		out[a.UID] = struct{}{}
	}
	for _, u := range a.AllowedUIDs {
		out[u] = struct{}{}
	}
	return out
}

// handleRootToken mints and returns a fresh root token. Never logs the value.
func (a *AdminServer) handleRootToken(w http.ResponseWriter, r *http.Request) {
	claims := a.Tokens.RootClaims()
	token, exp, err := a.Tokens.Sign(claims)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Error("admin socket: sign failure", slog.Any("err", err))
		}
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if a.Logger != nil {
		a.Logger.Info("admin socket: root token issued",
			slog.Time("expires_at", exp),
		)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"root_token": token,
		"expires_at": exp,
	})
}
