// Package gpicker drives Google's interactive Photos Picker API to let
// the dashboard owner select a set of photos once, downloads them all
// into the local cache during the picking session, and then exits. The
// dashboard's slideshow plays from the local cache afterwards — the
// picker session expires after ~24h and its baseUrls go with it.
package gpicker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const apiBase = "https://photospicker.googleapis.com/v1"

type pickingSession struct {
	ID            string `json:"id"`
	PickerURI     string `json:"pickerUri"`
	MediaItemsSet bool   `json:"mediaItemsSet"`
	ExpireTime    string `json:"expireTime"`
	PollingConfig struct {
		PollInterval string `json:"pollInterval"` // e.g. "5s"
		TimeoutIn    string `json:"timeoutIn"`
	} `json:"pollingConfig"`
}

type pickedItem struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	CreateTime string `json:"createTime"`
	MediaFile  struct {
		BaseURL  string `json:"baseUrl"`
		MimeType string `json:"mimeType"`
		Filename string `json:"filename"`
	} `json:"mediaFile"`
}

type mediaItemsResp struct {
	MediaItems    []pickedItem `json:"mediaItems"`
	NextPageToken string       `json:"nextPageToken"`
}

// Pick runs the full interactive flow: creates a session, opens the
// browser to the picker URL, polls until the user has picked their
// items, then downloads each into cacheDir.
func Pick(ctx context.Context, hc *http.Client, cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}

	session, err := createSession(ctx, hc)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Printf("Picker session: %s\nOpen this URL to pick photos:\n  %s\n", session.ID, session.PickerURI)
	_ = openBrowser(session.PickerURI)

	pollEvery := parseDur(session.PollingConfig.PollInterval, 5*time.Second)
	timeout := parseDur(session.PollingConfig.TimeoutIn, 30*time.Minute)
	deadline := time.Now().Add(timeout)

	for !session.MediaItemsSet {
		if time.Now().After(deadline) {
			return errors.New("picker session timed out before user finished picking")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
		s, err := getSession(ctx, hc, session.ID)
		if err != nil {
			fmt.Printf("(poll: %v)\n", err)
			continue
		}
		session = s
		if session.MediaItemsSet {
			break
		}
		fmt.Print(".")
	}
	fmt.Println()

	items, err := listMediaItems(ctx, hc, session.ID)
	if err != nil {
		return fmt.Errorf("list media items: %w", err)
	}
	fmt.Printf("Picked %d items, downloading to %s\n", len(items), cacheDir)

	// Wipe existing cache before downloading so a re-pick replaces the
	// slideshow rather than appending.
	if err := wipeCache(cacheDir); err != nil {
		fmt.Printf("(cache cleanup: %v)\n", err)
	}

	for i, it := range items {
		if !strings.HasPrefix(it.MediaFile.MimeType, "image/") {
			continue
		}
		path := filepath.Join(cacheDir, it.ID+".jpg")
		if err := download(ctx, hc, it.MediaFile.BaseURL+"=w2048-h1536", path); err != nil {
			fmt.Printf("  %d/%d  %s  FAILED: %v\n", i+1, len(items), it.MediaFile.Filename, err)
			continue
		}
		fmt.Printf("  %d/%d  %s\n", i+1, len(items), it.MediaFile.Filename)
	}

	// Polite cleanup: tell Google we're done with the session.
	_ = deleteSession(ctx, hc, session.ID)
	return nil
}

func createSession(ctx context.Context, hc *http.Client) (*pickingSession, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", apiBase+"/sessions", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", resp.Status, truncate(string(body), 300))
	}
	var s pickingSession
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func getSession(ctx context.Context, hc *http.Client, id string) (*pickingSession, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/sessions/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", resp.Status, truncate(string(body), 300))
	}
	var s pickingSession
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func deleteSession(ctx context.Context, hc *http.Client, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiBase+"/sessions/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func listMediaItems(ctx context.Context, hc *http.Client, sessionID string) ([]pickedItem, error) {
	var all []pickedItem
	pageToken := ""
	for {
		url := apiBase + "/mediaItems?pageSize=100&sessionId=" + sessionID
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("%s: %s", resp.Status, truncate(string(body), 300))
		}
		var r mediaItemsResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		all = append(all, r.MediaItems...)
		if r.NextPageToken == "" {
			break
		}
		pageToken = r.NextPageToken
	}
	return all, nil
}

func download(ctx context.Context, hc *http.Client, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: %s", resp.Status)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func wipeCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Wipe only files this picker wrote on previous runs. Manual
		// uploads use an "upload_" prefix and stay.
		if strings.HasPrefix(name, "upload_") {
			continue
		}
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".tmp") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

func parseDur(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	// Google returns "5s" or "120s" (Duration proto encoding).
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return fallback
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
