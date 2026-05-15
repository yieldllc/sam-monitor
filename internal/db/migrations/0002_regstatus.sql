-- 0002_regstatus.sql — registration status tracker (Task 11.5)

CREATE TABLE IF NOT EXISTS tracked_entity (
  uei                    TEXT PRIMARY KEY,
  name                   TEXT,
  last_status            TEXT,
  last_cage              TEXT,
  last_registration_date TIMESTAMPTZ,
  last_expiration_date   TIMESTAMPTZ,
  last_checked_at        TIMESTAMPTZ,
  added_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS status_event (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  uei          TEXT REFERENCES tracked_entity(uei),
  old_status   TEXT,
  new_status   TEXT,
  old_cage     TEXT,
  new_cage     TEXT,
  observed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  raw          JSONB
);
CREATE INDEX IF NOT EXISTS idx_status_event_observed ON status_event(observed_at DESC);

-- Seed Yield LLC (UEI from research/03-opportunity-research.md header).
INSERT INTO tracked_entity (uei, name) VALUES ('TA9TQJR2GL18', 'YIELD LLC')
ON CONFLICT (uei) DO NOTHING;
