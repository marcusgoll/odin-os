CREATE TABLE IF NOT EXISTS projection_freshness (
  surface TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  refreshed_at TEXT NOT NULL,
  details_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_projection_freshness_refreshed_at ON projection_freshness(refreshed_at);
