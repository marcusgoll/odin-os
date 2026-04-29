CREATE TABLE IF NOT EXISTS knowledge_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sha256 TEXT NOT NULL UNIQUE,
  size_bytes INTEGER NOT NULL,
  source_type TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  artifact_path TEXT NOT NULL,
  original_path TEXT NOT NULL,
  ocr_required INTEGER NOT NULL DEFAULT 0 CHECK (ocr_required IN (0, 1)),
  recorded_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  restricted INTEGER NOT NULL CHECK (restricted IN (0, 1)),
  source_kind TEXT NOT NULL,
  source_class TEXT NOT NULL CHECK (source_class IN ('markdown', 'text', 'machine_readable_pdf')),
  lifecycle TEXT NOT NULL CHECK (lifecycle IN ('declared', 'artifact_available', 'extracted', 'indexed', 'ready', 'stale', 'failed')),
  manifest_path TEXT NOT NULL UNIQUE CHECK (manifest_path GLOB 'memory/knowledge/*.md'),
  current_artifact_id INTEGER REFERENCES knowledge_artifacts(id) ON DELETE SET NULL,
  current_extraction_id INTEGER REFERENCES knowledge_extractions(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (source_class IN ('markdown', 'text', 'machine_readable_pdf'))
);

CREATE INDEX IF NOT EXISTS idx_knowledge_sources_scope ON knowledge_sources(scope, scope_key, key);
CREATE INDEX IF NOT EXISTS idx_knowledge_sources_lifecycle ON knowledge_sources(lifecycle, key);

CREATE TABLE IF NOT EXISTS knowledge_extractions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES knowledge_artifacts(id) ON DELETE CASCADE,
  extractor_name TEXT NOT NULL,
  extractor_version TEXT NOT NULL,
  status TEXT NOT NULL,
  failure_code TEXT NOT NULL DEFAULT '',
  failure_summary TEXT NOT NULL DEFAULT '',
  extracted_text_hash TEXT NOT NULL DEFAULT '',
  normalized_markdown_path TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_knowledge_extractions_source ON knowledge_extractions(source_id, id);
CREATE INDEX IF NOT EXISTS idx_knowledge_extractions_artifact ON knowledge_extractions(artifact_id);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  extraction_id INTEGER NOT NULL REFERENCES knowledge_extractions(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  text TEXT NOT NULL,
  anchor TEXT NOT NULL DEFAULT '',
  page_number INTEGER,
  restricted INTEGER NOT NULL CHECK (restricted IN (0, 1)),
  created_at TEXT NOT NULL,
  UNIQUE(extraction_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_source ON knowledge_chunks(source_id, extraction_id, ordinal);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
  source_key,
  title,
  topics,
  entities,
  chunk_text,
  tokenize='unicode61'
);

-- Derived from Knowledge Source Manifest metadata; not an authoritative registry.
CREATE TABLE IF NOT EXISTS knowledge_related_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  declared_by_manifest_path TEXT NOT NULL CHECK (declared_by_manifest_path GLOB 'memory/knowledge/*.md'),
  related_source_key TEXT NOT NULL,
  relationship TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(source_id, related_source_key, relationship)
);

CREATE TABLE IF NOT EXISTS restricted_knowledge_use_approvals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  use_type TEXT NOT NULL CHECK (use_type IN ('bulk_export', 'broad_extraction', 'sharing', 'executor_context_injection')),
  reason TEXT NOT NULL,
  decision TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  decided_by TEXT NOT NULL,
  decided_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_restricted_knowledge_approvals_source ON restricted_knowledge_use_approvals(source_id, decided_at);
