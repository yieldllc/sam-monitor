-- 0003_seed_searches.sql — seed the 4 saved searches from
-- research/03-opportunity-research.md §2. Idempotent: name has UNIQUE constraint
-- so re-running is a no-op. JSONB schema matches poller.SavedSearchQuery:
--   naics      []string      → ncode params
--   keywords   string        → q param
--   setAside   []string      → typeOfSetAside params (SBA=Total SB, SBP=Partial SB)
--   noticeType []string      → ptype params (k=Combined Synopsis/Solicitation,
--                              o=Solicitation, p=Presolicitation, r=Sources Sought,
--                              s=Special Notice)

INSERT INTO saved_search (name, query) VALUES
  ('A: DevSecOps + small business set-asides',
   '{
      "naics": ["541512","541511","541519","518210"],
      "keywords": "DevSecOps Kubernetes GitOps \"platform engineering\" \"infrastructure as code\"",
      "setAside": ["SBA","SBP"],
      "noticeType": ["o","k"]
    }'::jsonb),
  ('B: Sources Sought / RFI — same NAICS',
   '{
      "naics": ["541512","541511","541519","518210"],
      "noticeType": ["r","s","p"]
    }'::jsonb),
  ('C: DoD software factory keywords',
   '{
      "keywords": "\"Platform One\" \"Iron Bank\" \"Big Bang\" \"Kessel Run\" \"Black Pearl\" \"Software Factory\" \"Kobayashi Maru\" \"Space CAMP\""
    }'::jsonb),
  ('D: Cybersecurity small business set-aside',
   '{
      "naics": ["541512","541519","541690"],
      "keywords": "cybersecurity \"zero trust\" RMF ATO STIG",
      "setAside": ["SBA"],
      "noticeType": ["o","k"]
    }'::jsonb)
ON CONFLICT (name) DO NOTHING;
