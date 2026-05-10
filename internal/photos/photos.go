// Package photos polls an iCloud shared album webstream and caches images on disk.
//
// The shared album URL (e.g. https://www.icloud.com/sharedalbum/#A1B2C3D4) is
// served by Apple's webstream API at https://p<server>-sharedstreams.icloud.com.
// The poller calls "webstream" to list assets and "webasseturls" to resolve
// signed download URLs, then writes each new image into the cache dir.
//
// NOTE: The iCloud webstream API is not officially documented. This implementation
// is based on community reverse-engineering and may break if Apple changes the API.
// Use the local-folder fallback (empty or path-style AlbumURL) for verified usage.
package photos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
)

// Poller polls an iCloud shared album or scans a local directory for photos.
type Poller struct {
	AlbumURL string
	CacheDir string
	Refresh  time.Duration
	Store    *state.Store
	DB       *store.DB
	HTTP     *http.Client
}

// Once executes one poll cycle. If AlbumURL looks like a filesystem path
// (starts with "/" or "./") or is empty, we fall back to scanning CacheDir
// for image files instead of querying iCloud.
func (p *Poller) Once(ctx context.Context) error {
	if isLocalPath(p.AlbumURL) || p.AlbumURL == "" {
		return p.onceLocal(ctx)
	}
	return p.onceICloud(ctx)
}

func isLocalPath(s string) bool {
	if s == "" {
		return true
	}
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

// onceLocal scans CacheDir (or AlbumURL if it's a path override) for image files
// and reports them to state. Useful when iCloud is unavailable or for self-hosted
// setups without a shared album.
func (p *Poller) onceLocal(ctx context.Context) error {
	dir := p.CacheDir
	if isLocalPath(p.AlbumURL) && p.AlbumURL != "" {
		dir = p.AlbumURL
	}
	if dir == "" {
		// Nothing to scan, nothing to report.
		p.Store.SetPhotos(nil)
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			p.Store.SetPhotos(nil)
			return nil
		}
		return fmt.Errorf("read photos dir %s: %w", dir, err)
	}
	var photos []state.Photo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic":
			full := filepath.Join(dir, e.Name())
			photos = append(photos, state.Photo{ID: e.Name(), LocalPath: full})
		}
	}
	p.Store.SetPhotos(photos)
	return nil
}

// onceICloud talks to Apple's reverse-engineered webstream API.
// Tested only against documented samples — may break if Apple changes the API.
func (p *Poller) onceICloud(ctx context.Context) error {
	token, err := extractToken(p.AlbumURL)
	if err != nil {
		return err
	}
	server := serverFromToken(token)

	streamURL := fmt.Sprintf("https://%s-sharedstreams.icloud.com/%s/sharedstreams/webstream", server, token)
	body, err := p.post(ctx, streamURL, `{"streamCtag":null}`)
	if err != nil {
		return fmt.Errorf("webstream: %w", err)
	}
	var stream struct {
		Photos []struct {
			PhotoGUID   string `json:"photoGuid"`
			Derivatives map[string]struct {
				Checksum string `json:"checksum"`
				FileSize string `json:"fileSize"`
				URL      string `json:"url"`
			} `json:"derivatives"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(body, &stream); err != nil {
		return err
	}

	if err := os.MkdirAll(p.CacheDir, 0o755); err != nil {
		return err
	}

	var photos []state.Photo
	for _, ph := range stream.Photos {
		best := bestDerivative(ph.Derivatives)
		if best == "" {
			continue
		}
		signedURL, err := p.signURL(ctx, server, token, ph.Derivatives[best].Checksum)
		if err != nil {
			continue
		}
		fname := ph.PhotoGUID + filepath.Ext(strings.SplitN(signedURL, "?", 2)[0])
		if fname == ph.PhotoGUID {
			fname = ph.PhotoGUID + ".jpg"
		}
		dst := filepath.Join(p.CacheDir, fname)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			if err := p.download(ctx, signedURL, dst); err != nil {
				continue
			}
		}
		_ = p.DB.UpsertPhoto(ctx, store.Photo{
			ID: ph.PhotoGUID, URL: signedURL, LocalPath: dst, FetchedAt: time.Now(),
		})
		photos = append(photos, state.Photo{ID: ph.PhotoGUID, LocalPath: dst})
	}

	p.Store.SetPhotos(photos)
	return nil
}

// Run polls on the configured Refresh interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	if err := p.Once(ctx); err != nil {
		log.Printf("photos: poll: %v", err)
	}
	if p.Refresh == 0 {
		p.Refresh = 6 * time.Hour
	}
	t := time.NewTicker(p.Refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.Once(ctx); err != nil {
				log.Printf("photos: poll: %v", err)
			}
		}
	}
}

var tokenRe = regexp.MustCompile(`#([A-Za-z0-9]+)`)

func extractToken(s string) (string, error) {
	m := tokenRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", fmt.Errorf("no token in %q", s)
	}
	return m[1], nil
}

// serverFromToken picks the Apple shard. The first character of the token's
// SHA1 (mod 6) maps to a shared-streams server number; this is the documented
// mechanism in community write-ups.
func serverFromToken(token string) string {
	sum := sha1.Sum([]byte(token))
	h := hex.EncodeToString(sum[:])
	switch h[0] {
	case '0', '1':
		return "p01"
	case '2', '3':
		return "p02"
	case '4', '5':
		return "p03"
	case '6', '7':
		return "p04"
	case '8', '9', 'a':
		return "p05"
	default:
		return "p06"
	}
}

func bestDerivative(d map[string]struct {
	Checksum string `json:"checksum"`
	FileSize string `json:"fileSize"`
	URL      string `json:"url"`
}) string {
	var best string
	var bestSize int64
	for k, v := range d {
		var n int64
		fmt.Sscan(v.FileSize, &n)
		if n > bestSize {
			bestSize = n
			best = k
		}
	}
	return best
}

func (p *Poller) post(ctx context.Context, urlStr, body string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", urlStr, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := p.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (p *Poller) signURL(ctx context.Context, server, token, checksum string) (string, error) {
	u := fmt.Sprintf("https://%s-sharedstreams.icloud.com/%s/sharedstreams/webasseturls", server, token)
	body := fmt.Sprintf(`{"photoGuids":["%s"]}`, checksum)
	resp, err := p.post(ctx, u, body)
	if err != nil {
		return "", err
	}
	var r struct {
		Items map[string]struct {
			URL string `json:"url_location"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return "", err
	}
	for _, v := range r.Items {
		return v.URL, nil
	}
	return "", fmt.Errorf("no signed url")
}

func (p *Poller) download(ctx context.Context, urlStr, dst string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	client := p.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("download %s: status %d", urlStr, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
