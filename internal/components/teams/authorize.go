package teams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// devicePollMaxInterval caps how slow AAD can tell us to back off. The spec
// allows the server to return slow_down repeatedly; we bound it so a hostile
// (or misbehaving) endpoint cannot freeze the flow indefinitely.
const devicePollMaxInterval = 30 * time.Second

// deviceCodeResponse mirrors the wire shape of the /devicecode endpoint.
// The `interval` field is seconds; ExpiresIn / Interval default to documented
// values when omitted.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message,omitempty"`
}

// Authorize runs the OAuth2 device-code flow against the configured tenant
// and persists the resulting refresh token to cfg.TokenFile. Designed to run
// interactively from the operator's terminal — the prompt is written to
// stderr so it never collides with stdout-piped tooling.
//
// The flow:
//
//  1. POST /{tenant}/oauth2/v2.0/devicecode with client_id + scope list
//  2. Print AAD's `message` (which includes the URL + user code)
//  3. Poll /{tenant}/oauth2/v2.0/token every `interval` seconds until
//     authorization completes, expires, or is denied
//  4. Save the resulting access + refresh tokens to disk
//
// The function returns nil on success; non-nil errors include the AAD
// rejection code where applicable.
func Authorize(ctx context.Context, cfg Config, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if cfg.AuthMode != "" && !strings.EqualFold(cfg.AuthMode, "delegated") {
		return fmt.Errorf("teams: authorize requires auth_mode=delegated, got %q", cfg.AuthMode)
	}
	if cfg.TokenFile == "" {
		return errors.New("teams: authorize requires token_file to be configured")
	}
	if len(cfg.Scopes) == 0 {
		return errors.New("teams: authorize requires at least one scope")
	}
	httpc := &http.Client{Timeout: cfg.RequestTimeout}
	if cfg.RequestTimeout == 0 {
		httpc.Timeout = 15 * time.Second
	}

	dc, err := requestDeviceCode(ctx, httpc, cfg)
	if err != nil {
		return err
	}

	if dc.Message != "" {
		_, _ = fmt.Fprintln(out, dc.Message)
	} else {
		_, _ = fmt.Fprintf(out, "Open %s in a browser and enter code %s.\n", dc.VerificationURI, dc.UserCode)
	}

	tok, err := pollForToken(ctx, httpc, cfg, dc)
	if err != nil {
		return err
	}

	saved := cachedToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Scope:        tok.Scope,
		ObtainedAt:   time.Now(),
		TenantID:     cfg.TenantID,
		ClientID:     cfg.ClientID,
	}
	if saved.RefreshToken == "" {
		return errors.New("teams: authorize succeeded but AAD returned no refresh_token (was offline_access in the scope list?)")
	}
	if err := newTokenStore(cfg.TokenFile).Save(saved); err != nil {
		return fmt.Errorf("teams: persist token: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Authorization complete. Refresh token persisted to %s.\n", cfg.TokenFile)
	return nil
}

// requestDeviceCode issues the initial /devicecode call and returns the
// poller parameters.
func requestDeviceCode(ctx context.Context, httpc *http.Client, cfg Config) (deviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("scope", strings.Join(cfg.Scopes, " "))

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", cfg.LoginBase, cfg.TenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("teams: build devicecode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("teams: devicecode request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("teams: read devicecode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var aaErr graphTokenError
		if jerr := json.Unmarshal(raw, &aaErr); jerr == nil && aaErr.Error != "" {
			return deviceCodeResponse{}, fmt.Errorf("teams: devicecode %d %s: %s", resp.StatusCode, aaErr.Error, aaErr.ErrorDescription)
		}
		return deviceCodeResponse{}, fmt.Errorf("teams: devicecode %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var dc deviceCodeResponse
	if err := json.Unmarshal(raw, &dc); err != nil {
		return deviceCodeResponse{}, fmt.Errorf("teams: decode devicecode response: %w", err)
	}
	if dc.DeviceCode == "" || dc.VerificationURI == "" {
		return deviceCodeResponse{}, errors.New("teams: devicecode response missing device_code or verification_uri")
	}
	if dc.Interval <= 0 {
		// RFC 8628 default — 5s.
		dc.Interval = 5
	}
	if dc.ExpiresIn <= 0 {
		dc.ExpiresIn = 900 // 15 minutes
	}
	return dc, nil
}

// pollForToken polls the token endpoint at the cadence the device-code
// response specified. It honours AAD's authorization_pending / slow_down
// signals and aborts on expired_token / access_denied / context cancellation.
func pollForToken(ctx context.Context, httpc *http.Client, cfg Config, dc deviceCodeResponse) (graphTokenResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", cfg.LoginBase, cfg.TenantID)
	interval := time.Duration(dc.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return graphTokenResponse{}, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("client_id", cfg.ClientID)
		form.Set("device_code", dc.DeviceCode)
		if cfg.ClientSecret != "" {
			form.Set("client_secret", cfg.ClientSecret)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return graphTokenResponse{}, fmt.Errorf("teams: build token poll: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := httpc.Do(req)
		if err != nil {
			return graphTokenResponse{}, fmt.Errorf("teams: token poll: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return graphTokenResponse{}, fmt.Errorf("teams: read token poll response: %w", readErr)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var tr graphTokenResponse
			if err := json.Unmarshal(raw, &tr); err != nil {
				return graphTokenResponse{}, fmt.Errorf("teams: decode token response: %w", err)
			}
			if tr.AccessToken == "" {
				return graphTokenResponse{}, errors.New("teams: token response missing access_token")
			}
			return tr, nil
		}

		var aaErr graphTokenError
		_ = json.Unmarshal(raw, &aaErr)
		switch aaErr.Error {
		case "authorization_pending":
			// User hasn't completed the prompt yet — keep polling.
			continue
		case "slow_down":
			// Back off by the documented 5s; cap to devicePollMaxInterval.
			interval += 5 * time.Second
			if interval > devicePollMaxInterval {
				interval = devicePollMaxInterval
			}
			continue
		case "expired_token":
			return graphTokenResponse{}, fmt.Errorf("teams: device code expired before authorization completed")
		case "access_denied":
			return graphTokenResponse{}, fmt.Errorf("teams: authorization denied by user")
		}
		if aaErr.Error != "" {
			return graphTokenResponse{}, fmt.Errorf("teams: token poll %d %s: %s", resp.StatusCode, aaErr.Error, aaErr.ErrorDescription)
		}
		return graphTokenResponse{}, fmt.Errorf("teams: token poll %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return graphTokenResponse{}, errors.New("teams: device code expired before authorization completed")
}
