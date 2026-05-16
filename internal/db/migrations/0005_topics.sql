-- 0005_topics.sql — SBIR/STTR open-topic poller schema
--
-- One row per topic the poller has ever observed, keyed by (source, topic_code).
-- `raw` preserves the full upstream payload so we never have to re-fetch.
-- Indexes: close_at for the dashboard's "sort by deadline" view, status for
-- triage filters.

CREATE TABLE IF NOT EXISTS topic (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  source        TEXT NOT NULL,
  topic_code    TEXT NOT NULL,
  title         TEXT NOT NULL,
  agency        TEXT,
  phase         TEXT,
  open_at       TIMESTAMPTZ,
  close_at      TIMESTAMPTZ,
  abstract      TEXT,
  url           TEXT,
  keywords_hit  TEXT[],
  raw           JSONB NOT NULL,
  status        TEXT NOT NULL DEFAULT 'new',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source, topic_code)
);
CREATE INDEX IF NOT EXISTS topic_close_at_idx ON topic(close_at);
CREATE INDEX IF NOT EXISTS topic_status_idx  ON topic(status);
