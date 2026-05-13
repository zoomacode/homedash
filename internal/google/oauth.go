// Package google handles OAuth 2.0 for Google APIs used by homedash:
// the initial browser-based authorization (one-time, run from the Mac),
// the on-disk token cache, and the http.Client factory that auto-refreshes.
package google

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scopes requested by homedash. `tasks` (read+write) lets the iPad tick
// reminders off; `calendar.readonly` covers events.list;
// `photospicker.mediaitems.readonly` enables the user-driven photo picker
// flow (the modern replacement for the deprecated Photos Library API
// access to user library data).
var Scopes = []string{
	"https://www.googleapis.com/auth/tasks",
	"https://www.googleapis.com/auth/calendar.readonly",
	"https://www.googleapis.com/auth/photospicker.mediaitems.readonly",
}

// Config is the static OAuth client identity (from secrets.env) plus the
// on-disk path where the user's refresh-capable token is cached.
type Config struct {
	ClientID     string
	ClientSecret string
	TokenFile    string
}

// oauthConfig builds the *oauth2.Config used by both the initial auth dance
// and the silent token refresh.
func (c Config) oauthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       Scopes,
		RedirectURL:  redirectURL,
	}
}

// Client returns an *http.Client that automatically attaches and refreshes
// the cached OAuth token. Returns an error if no token has been saved yet.
func (c Config) Client(ctx context.Context) (*http.Client, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, errors.New("google: missing GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET")
	}
	tok, err := loadToken(c.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("google: load token: %w", err)
	}
	cfg := c.oauthConfig("")
	src := cfg.TokenSource(ctx, tok)
	// Persist refreshed tokens back to disk so a restart doesn't have to
	// refresh again immediately.
	src = &savingTokenSource{src: src, path: c.TokenFile, last: tok}
	return oauth2.NewClient(ctx, src), nil
}

// Authorize runs the one-time browser-based OAuth dance: opens the consent
// URL in the default browser, captures the redirect on a loopback HTTP
// server, exchanges the code for tokens, and writes the result to disk.
func (c Config) Authorize(ctx context.Context) error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("google: missing GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET")
	}
	if c.TokenFile == "" {
		return errors.New("google: TokenFile not set")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	cfg := c.oauthConfig(redirectURL)

	state := randString(24)
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"))

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			done <- result{err: fmt.Errorf("oauth error: %s", e)}
			fmt.Fprintf(w, "Google OAuth error: %s\nYou can close this window.", e)
			return
		}
		if q.Get("state") != state {
			done <- result{err: errors.New("state mismatch")}
			fmt.Fprint(w, "State mismatch. You can close this window.")
			return
		}
		code := q.Get("code")
		if code == "" {
			done <- result{err: errors.New("missing code")}
			fmt.Fprint(w, "Missing code. You can close this window.")
			return
		}
		done <- result{code: code}
		fmt.Fprint(w, "Done. You can close this window and return to the terminal.")
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(listener)
	defer srv.Shutdown(context.Background())

	fmt.Println("Opening browser for Google authorization. If it doesn't open, visit:")
	fmt.Println(authURL)
	_ = openBrowser(authURL)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-done:
		if r.err != nil {
			return r.err
		}
		tok, err := cfg.Exchange(ctx, r.code)
		if err != nil {
			return fmt.Errorf("token exchange: %w", err)
		}
		if scope := tok.Extra("scope"); scope != nil {
			fmt.Printf("Granted scopes: %v\n", scope)
		}
		return saveToken(c.TokenFile, tok)
	case <-time.After(5 * time.Minute):
		return errors.New("oauth: timed out waiting for browser callback")
	}
}

// savingTokenSource wraps an oauth2.TokenSource so refreshed tokens get
// persisted back to disk for the next process start.
type savingTokenSource struct {
	src  oauth2.TokenSource
	path string
	last *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if s.last == nil || t.AccessToken != s.last.AccessToken || t.RefreshToken != s.last.RefreshToken {
		// Preserve the original refresh_token if the new token didn't include
		// one (Google often omits it on refresh).
		if t.RefreshToken == "" && s.last != nil {
			t.RefreshToken = s.last.RefreshToken
		}
		_ = saveToken(s.path, t)
		s.last = t
	}
	return t, nil
}

func loadToken(path string) (*oauth2.Token, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveToken(path string, t *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return errors.New("unsupported OS")
	}
	return cmd.Start()
}

func randString(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
