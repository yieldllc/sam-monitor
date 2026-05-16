-- 0006_seed_topic_keywords.sql — keywords the topic poller searches with.
--
-- The poller reads this list at runtime instead of hardcoding so we can add
-- new keywords without a redeploy. `enabled = false` retires a keyword
-- without losing history of past topic matches.

CREATE TABLE IF NOT EXISTS topic_keyword (
  keyword     TEXT PRIMARY KEY,
  enabled     BOOLEAN NOT NULL DEFAULT true,
  added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO topic_keyword (keyword) VALUES
  ('container'),
  ('hardened image'),
  ('SBOM'),
  ('supply chain'),
  ('provenance'),
  ('software supply chain'),
  ('Iron Bank'),
  ('Platform One'),
  ('reproducible build'),
  ('attestation'),
  ('cATO')
ON CONFLICT (keyword) DO NOTHING;
