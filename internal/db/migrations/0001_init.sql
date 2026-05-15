-- 0001_init.sql — core schema for sam-monitor (design spec §4)

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS saved_search (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name            TEXT NOT NULL UNIQUE,
  query           JSONB NOT NULL,
  enabled         BOOLEAN NOT NULL DEFAULT true,
  last_polled_at  TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS opportunity (
  notice_id        TEXT PRIMARY KEY,
  solicitation_no  TEXT,
  title            TEXT NOT NULL,
  agency           TEXT,
  naics            TEXT[],
  psc              TEXT[],
  set_aside        TEXT,
  notice_type      TEXT,
  posted_at        TIMESTAMPTZ,
  response_due_at  TIMESTAMPTZ,
  place_of_perf    TEXT,
  description      TEXT,
  url              TEXT,
  raw              JSONB,
  saved_search_id  UUID REFERENCES saved_search(id),
  first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  status           TEXT NOT NULL DEFAULT 'new'
);
CREATE INDEX IF NOT EXISTS idx_opp_status_posted ON opportunity(status, posted_at DESC);
CREATE INDEX IF NOT EXISTS idx_opp_due ON opportunity(response_due_at) WHERE response_due_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS prime_poc (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  prime         TEXT NOT NULL,
  contact_name  TEXT,
  contact_email TEXT,
  contact_url   TEXT,
  programs      TEXT[],
  notes         TEXT,
  added_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS interaction (
  id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  opportunity_id    TEXT REFERENCES opportunity(notice_id),
  prime_poc_id      UUID REFERENCES prime_poc(id),
  kind              TEXT NOT NULL,
  occurred_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  follow_up_due_at  TIMESTAMPTZ,
  notes             TEXT
);
