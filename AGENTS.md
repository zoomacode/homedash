# AGENTS.md

Context for AI coding agents working in this repository. Humans should read
[README.md](README.md) instead; this file is for tooling.

## What this project is

A self-hosted dashboard that runs on an iPad Air mounted in a kitchen. Single
Go binary serves HTML over an SSE-driven htmx UI. Iterating on it usually
means: add a new data source, tweak the iPad layout, or fix an integration
that broke because a vendor (Google, Apple, Todoist) changed their API.

## Commands

```sh
make build            # templ generate + go build → dist/homedash
make build-pi         # cross-compile for the Pi (linux/arm64)
make test             # go test ./...
make deploy           # rsync + systemd restart on the Pi
make doctor           # prints Pi-side setup checklist

templ generate        # regenerate *_templ.go from *.templ — run this
                      # whenever you edit a templ file before `go build`
```

## Layout

- `cmd/homedash/main.go` — entrypoint + subcommand dispatch
  (`auth-google`, `pick-photos`). All goroutine wiring lives here.
- `internal/config/` — yaml + env loading. Add a new vendor here.
- `internal/state/` — atomic `*Snapshot` swap; `SetXFromSource` for
  multi-source merging.
- `internal/{caldav,gcal,gtasks,gpicker,todoist,mqttsub,weather,rss,photos}/`
  — one package per data source. Each implements a poller (`Run(ctx, interval)`)
  and most write into state via `Set*FromSource(...)`.
- `internal/web/` — chi router, SSE, `/fragment/*` endpoints for htmx
  swaps. Templates in `internal/web/templates/*.templ`. Static assets in
  `internal/web/static/` are embedded via `go:embed`.

## How to add a new data source

Use the existing sources as patterns; the Todoist commit (`bbfcb95`) and
the original Google migration (`c540c02`) are good references.

1. New package under `internal/<source>/` with:
   - A `Client` struct, `New(...)` constructor
   - A `Run(ctx context.Context, every time.Duration)` poller
   - A `PollOnce(ctx)` that fetches once and calls
     `store.SetXFromSource("<source>", items)`
   - For reminder-sources only: implement
     `ToggleReminder(ctx, uid, done) error` — picked up by the dispatcher.
2. Add config in `internal/config/config.go` (yaml fields + env-loaded
   secrets in `Load`).
3. Wire it in `cmd/homedash/main.go` — usually 5-10 lines.
4. If it's a reminders source: stamp `Reminder.Source = "<name>"` on
   each emitted item and add `dispatch.bySource["<name>"] = client` in
   `main.go` so the dispatcher routes toggles back to your client.
5. Update [README.md](README.md) source table, and
   [deploy/config.example.yaml](deploy/config.example.yaml).

## State conventions

- `Snapshot` is read via `store.Snapshot()` which returns a copy; treat
  it as read-only. Writes go through `Set*` methods which atomically
  swap a new `*Snapshot` and notify SSE subscribers.
- Reminders + Events use source buckets so multiple pollers can coexist
  without overwriting each other:
  `store.SetRemindersFromSource("todoist", ...)`.
- `SetRemindersFromSource` preserves items completed within the
  dashboard's 5-min grace window — needed for sources whose list
  endpoint returns active items only.

## UI conventions

- Templates are templ files (`*.templ`); the generated `*_templ.go`
  files are committed. **You must run `templ generate` after editing
  any `.templ` file** or `go build` will keep using the old version.
- Don't add CSS frameworks. The single `styles.css` uses CSS variables
  + light/dark themes via `[data-theme]` on `<html>`.
- Layout target is iPad Air portrait (~820×1180). Whole page is sized
  with `height: 100vh`; long content scrolls inside its section.
- HTMX over a single SSE channel: each section card has
  `hx-trigger="sse:<section>"` and the server pushes the section name
  whenever state for that section changes (`store.notify("<section>")`).

## Patterns that have caused bugs already

- **iCloud's REPORT** for VTODO returns the collection itself with a
  404 calendar-data property. `go-webdav` bails on the whole response
  on that 404. The reminders fetch in `internal/caldav/client.go` does
  a raw REPORT + XML parse to work around it.
- **iCloud's REPORT for VEVENT** with `<allprop/>` returns empty VEVENTs.
  Use explicit prop list. Same file.
- **Google Tasks API**: marshalling a `Task` with `NullFields=["Completed"]`
  AND a non-empty Completed value errors. Use `Tasks.Patch` with a minimal
  `&tasks.Task{Status: ...}` instead of Get+Update.
- **Google Photos Library API** is restricted to app-created data for
  projects created after Mar 31, 2025. We don't use it; the Picker API
  (`gpicker/`) is the supported path.
- **Drive's `drive.photos.readonly` scope** still exists but Drive v3
  doesn't support `spaces=photos` anymore — verified via discovery doc.
  Not a usable workaround.
- **htmx forms don't submit on checkbox click** unless you add
  `hx-trigger="change"` (the natural form trigger is `submit`, which a
  checkbox click never fires). The reminder toggle form has this — keep
  it when refactoring.
- **iPad Safari caches static assets aggressively**. The server sends
  `Cache-Control: no-store, must-revalidate` for `/static/*` so a
  redeploy doesn't leave the iPad on stale CSS/JS. Keep that header.

## Don't

- Don't introduce a JS framework or build step. htmx + a few hand-rolled
  scripts in `static/` is the contract.
- Don't add commentary comments. Per repo style: only comment when the
  *why* would surprise a reader (constraints, vendor quirks, bug refs).
  Function/variable names should carry the *what*.
- Don't replace the `SetXFromSource` merge with single-source writes.
  Multiple pollers will overwrite each other if you do.
- Don't widen the photo cache by storing originals — picker downloads
  with `=w2048-h1536`; iPad doesn't need more.
