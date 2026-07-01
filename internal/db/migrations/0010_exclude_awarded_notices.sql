-- 0010_exclude_awarded_notices.sql — stop surfacing contracts that are already
-- awarded or being sole-sourced.
--
-- Searches A/B/D already carry a noticeType (ptype) allow-list, so they never
-- pulled Award ('a') or Justification ('u') notices. Search C had no noticeType
-- filter, so it ingested them. Give C the biddable/market-research allow-list:
--   k = Combined Synopsis/Solicitation   o = Solicitation   p = Presolicitation
--   r = Sources Sought                   s = Special Notice
-- Award Notice and Justification (J&A, sole-source intent) are deliberately
-- omitted — neither is competable.
--
-- Idempotent: only sets noticeType when absent.
UPDATE saved_search
SET query = jsonb_set(query, '{noticeType}', '["k","o","p","r","s"]'::jsonb, true)
WHERE name = 'C: DoD software factory keywords'
  AND NOT (query ? 'noticeType');
