# homedash — Design

A self-hosted home dashboard running on a Raspberry Pi. Browser-based, served on the LAN, displaying calendar, weather, live MQTT sensor data, todos, photos, and news.

## Goals

- **Simple**: a single Go binary, one config file, one `systemd` unit. No HA, no Node, no Docker.
- **Fast**: cold start <1s, ~20–30 MB resident, sub-100ms live MQTT round-trip.
- **Phone-first**: primary view is a phone in the kitchen / hallway. Laptop and tablet just work.
- **Source of truth on the phone**: iCloud holds calendar + reminders; the dashboard reads/writes via CalDAV.

## Non-goals

- Not a home automation platform (no rules engine, no scenes, no device control).
- Not multi-user. One household, one config, no auth on day 1.
- Not internet-exposed. LAN-only. Remote access is the user's problem (Tailscale, reverse proxy).
- No SPA, no node_modules, no build pipeline beyond `go build`.

## Constraints

- Runs on a Raspberry Pi with limited RAM/CPU. Single binary, no extra services beyond the existing Mosquitto MQTT broker.
- iCloud is the user's calendar/reminders/photos source. App-specific password lives on the Pi as an env var.
- Network is LAN-only; no TLS terminated by the app on day 1.

## Architecture

One Go process. Pollers + MQTT subscriber feed an in-memory `state`. The HTTP layer renders templ templates server-side; HTMX swaps fragments in response to user actions. An SSE channel pushes `state` changes to all connected browsers so the dashboard updates without polling from the client.

```
┌─────────────────────── homedash (Go binary) ──────────────────────┐
│                                                                   │
│  ┌─ pollers ──┐  ┌─ mqtt sub ─┐  ┌─ store ─┐                      │
│  │ caldav     │  │ paho client │ │ sqlite  │                      │
│  │ open-meteo │  │ → in-mem    │ │ (cache, │                      │
│  │ rss        │  │   sensors   │ │  photos │                      │
│  │ icloud-alb │  │             │ │  meta)  │                      │
│  └─────┬──────┘  └──────┬──────┘ └────┬────┘                      │
│        └────────────┬───┴──────────────┘                          │
│                     │                                             │
│              ┌──────▼──────┐                                      │
│              │  app state  │  read-only snapshots                 │
│              └──────┬──────┘                                      │
│        ┌────────────┴───────────┐                                 │
│        │                        │                                 │
│  ┌─────▼─────┐         ┌────────▼────────┐                        │
│  │ http      │         │ sse broadcaster │                        │
│  │ templ+htmx│         │                 │                        │
│  └───────────┘         └─────────────────┘                        │
└───────────────────────────────────────────────────────────────────┘
                              │
                       browser (HTMX)
```

## Tech stack

| Concern | Choice | Reason |
|---|---|---|
| Language | Go (1.22+) | speed, single binary, easy ARM cross-compile |
| HTTP | `go-chi/chi` | minimal router, idiomatic |
| Templates | `a-h/templ` | type-safe, compiled, no runtime template loading |
| Frontend behaviour | HTMX | server-rendered with progressive enhancement, no SPA build |
| Live updates | Server-Sent Events (SSE) | one-way push fits the dashboard model; simpler than WebSockets |
| MQTT client | `eclipse/paho.mqtt.golang` | mature, widely used |
| CalDAV | `emersion/go-webdav` | maintained, supports iCloud with app-specific password |
| Weather | Open-Meteo HTTP API | free, keyless, accurate |
| RSS | `mmcdole/gofeed` | de-facto Go feed parser |
| SQLite | `modernc.org/sqlite` | pure Go — clean ARM cross-compile, no CGO |
| Config | `gopkg.in/yaml.v3` | one YAML file |

## Modules

Each is its own Go package under `internal/`. Each has a small public interface and is independently testable.

### `mqtt`

Subscribes to topics declared in config. Parses payloads (numeric, JSON, or string) and writes the latest reading per topic into `state`. Reconnects with exponential backoff on broker disconnect. Does not persist; on restart the latest values come in again from the broker.

### `caldav`

Polls iCloud (every 15 min by default) for events from the configured calendars and reminders from the configured reminders list. Exposes:

- `UpcomingEvents(window time.Duration) []Event`
- `Reminders() []Reminder`
- `ToggleReminder(uid string) error`

Writes for `ToggleReminder` go through CalDAV PUT. The poll picks up external edits; the toggle path also updates `state` optimistically so the UI feels instant.

### `weather`

Polls Open-Meteo every 30 min for the configured lat/lon. Returns: now (temp, condition icon, feels-like) + 5-day forecast (high/low/icon).

### `rss`

Polls the configured feed list every 15 min. Deduplicates by GUID. Caches recent items in SQLite so a transient outage doesn't blank the section.

### `photos`

Polls the iCloud shared album webstream (every 6h by default), downloads new images to `/var/lib/homedash/photos/`, records metadata in SQLite. The frontend cycles through cached images client-side (8s interval) — no server work per slide.

### `store`

SQLite layer. Schema is small:

- `rss_items(guid, feed, title, link, published_at, fetched_at)`
- `photos(id, url, local_path, fetched_at)`

That's it. Calendar and reminders are not cached — they live in iCloud and are re-fetched on the poll cadence.

### `state`

Shared in-memory snapshot. One struct per concern (sensors, events, reminders, weather, etc.). Reads are lock-free via `atomic.Value`; writes are infrequent and copy-on-write. Emits a `Changed(section)` event to subscribers (the SSE broadcaster).

### `web`

HTTP routes:

| Route | Method | Purpose |
|---|---|---|
| `/` | GET | full page render |
| `/events` | GET | SSE endpoint, streams change events |
| `/reminders/{uid}/toggle` | POST | toggle reminder, returns updated `<li>` fragment |
| `/photo/{id}` | GET | serve a cached photo file |
| `/healthz` | GET | for systemd/monitoring |

Each section of the page is a templ component that can be rendered standalone (so SSE swaps fetch only the affected fragment).

### `config`

Reads `config.yaml` from `/etc/homedash/config.yaml` (override with `-config` flag). Reads secrets from env: `ICLOUD_USER`, `ICLOUD_APP_PASSWORD`. Validates on startup; refuses to run with bad config.

### `cmd/homedash`

Wires the modules, starts pollers and the HTTP server, handles SIGTERM with a graceful shutdown.

## Configuration

`/etc/homedash/config.yaml`:

```yaml
location:
  lat: 50.08
  lon: 14.43

http:
  listen: ":8080"

mqtt:
  broker: "tcp://localhost:1883"
  client_id: "homedash"
  topics:
    - topic: "weather/outdoor/temp"
      name: "Outdoor Temp"
      unit: "°C"
      group: "outdoor"      # free-form label; sensors with the same group render together
      decimals: 1
      stale_after: "5m"     # show "stale" badge if no message in this window
    - topic: "sensors/livingroom/humidity"
      name: "Living Room Humidity"
      unit: "%"
      group: "indoor"
      decimals: 0
      stale_after: "10m"

calendars:
  poll_minutes: 15
  include:
    - "Personal"
    - "Family"

reminders:
  list_name: "Dashboard"

weather:
  poll_minutes: 30

rss:
  poll_minutes: 15
  feeds:
    - "https://example.com/feed.xml"

photos:
  shared_album_url: "https://www.icloud.com/sharedalbum/#XXXXXXX"
  refresh_hours: 6
  cache_dir: "/var/lib/homedash/photos"
  slideshow_seconds: 8
```

`systemd` `EnvironmentFile=/etc/homedash/secrets.env`:

```
ICLOUD_USER=you@icloud.com
ICLOUD_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
```

## Data flow

**Live (push):** MQTT message → `mqtt` decodes → `state` writes new sensor value → `state.Changed("sensors")` → SSE broadcaster sends event to all clients → HTMX swaps `<div id="sensors">` partial. End-to-end: 50–100 ms.

**Poll (pull):** poller ticker fires → fetch external API → `state` write → SSE notify → fragment swap. The browser does no polling itself.

**Write (reminder toggle):** click `<input type="checkbox">` → HTMX POST `/reminders/{uid}/toggle` → handler calls `caldav.ToggleReminder` → `state` updated optimistically → handler returns the new `<li>` HTML. Other clients see the change via SSE.

## Layout

Phone-first, single column. Sections in this order:

1. Clock + date
2. Current weather (Open-Meteo) with today's high/low and 5-day strip
3. Outdoor sensors (pinned MQTT group: weather station)
4. Today's events + tomorrow's events
5. Reminders (tap to toggle)
6. Indoor sensors (other MQTT groups)
7. Photo slideshow
8. News headlines

On viewports ≥ 900px, sections rearrange into a 2-column grid via CSS grid; ≥ 1400px, three columns. No JS layout logic.

Theme: dark by default (kitchen-friendly at night), with a `prefers-color-scheme` light variant.

## Error handling

- A poller failure logs and leaves the previous `state` snapshot intact. Polled sections (weather, calendar, reminders, RSS) get a "stale" badge if data is older than `2 × poll_interval`. MQTT sensors get a "stale" badge if no message has arrived within their per-topic `stale_after` window.
- MQTT broker disconnect triggers exponential backoff reconnect (1s → 60s cap). Sensor section shows "disconnected" until reconnected.
- iCloud 401 → red banner on the page ("Reauthenticate iCloud") so the user notices and rotates the app password.
- Photo download failure: keep serving the previous cached set; log and retry next cycle.
- The binary itself does not crash on transient errors. A panic in any goroutine is recovered with a log and the goroutine restarted.

## Testing

| Scope | Approach |
|---|---|
| Pure parsers (MQTT payload decode, RSS parse) | unit tests, table-driven |
| `mqtt` package | spin up a local Mosquitto in test (or `mochi-mqtt/server` embedded) |
| `caldav` package | run `emersion/go-webdav`'s test server, exercise read + write |
| `weather`, `rss` | `httptest.Server` returning canned bodies |
| `web` handlers | `httptest`, assert templ output contains expected fragments |
| End-to-end | one smoke test: boot the binary against fake MQTT + fake CalDAV, GET `/`, assert key fragments present |

CI runs `go vet`, `go test ./...`, `templ generate --diff`, and a cross-compile to `linux/arm64` to catch ARM-only build breaks.

## Deployment

Targets a Raspberry Pi 4/5 running 64-bit Raspberry Pi OS.

```
make build-pi      # GOOS=linux GOARCH=arm64 go build -o dist/homedash ./cmd/homedash
make deploy        # rsync dist/homedash + config + service file to the pi
ssh pi 'sudo systemctl restart homedash'
```

`homedash.service` (excerpt):

```
[Service]
Type=simple
ExecStart=/usr/local/bin/homedash -config /etc/homedash/config.yaml
EnvironmentFile=/etc/homedash/secrets.env
Restart=always
User=homedash
StateDirectory=homedash
```

Listens on `:8080`. Accessed as `http://homedash.local:8080` (mDNS) or by IP.

## Open questions for later iterations

These are deliberately out of scope for v1 but worth flagging:

- Auth / remote access (Tailscale is the obvious answer)
- Per-room/per-device variant layouts (e.g., a hallway-only "next event + weather" view)
- Recording MQTT history for graphs (would need real time-series storage; SQLite is fine to start)
- Adding indoor air quality, energy, etc. — already covered by the generic MQTT topic config

## Future-proofing notes

- Config schema is versioned via a top-level `version: 1` field so we can migrate later.
- All polling intervals are config-driven so we can tune in production without recompiling.
- The `state` package abstracts the storage so we could swap in Redis or a time-series DB later without touching pollers or web layer.
