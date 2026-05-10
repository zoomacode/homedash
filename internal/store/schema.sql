CREATE TABLE IF NOT EXISTS rss_items (
  guid       TEXT PRIMARY KEY,
  feed       TEXT NOT NULL,
  title      TEXT NOT NULL,
  link       TEXT NOT NULL,
  published  TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS rss_published_idx ON rss_items(published DESC);

CREATE TABLE IF NOT EXISTS photos (
  id         TEXT PRIMARY KEY,
  url        TEXT NOT NULL,
  local_path TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);
