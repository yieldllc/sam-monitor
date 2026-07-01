-- 0008_software_psc_filter.sql — focus the poller on software / IT work and cut
-- the HVAC/equipment firehose.
--
-- Root cause: 'C: DoD software factory keywords' had NO naics filter and relied
-- on SAM.gov's free-text `q` param, which the /opportunities/v2/search endpoint
-- silently ignores — so it ingested the entire opportunity feed (~50k rows,
-- >97% refrigerators/valves/medical). Two fixes:
--
--   1. includePSC: a client-side Product Service Code allow-list (poller matches
--      by case-insensitive prefix). Group "D" = IT & telecom services;
--      "7A"/"7B"/"7J" = business-app / system / security software. This is the
--      dependable "software, not HVAC" signal.
--   2. Give search C a software naics filter so SAM constrains volume at the
--      source (below the 5000/cycle cap) instead of dumping the whole feed.
--
-- Idempotent: each UPDATE only sets a key when absent. Search B (Sources Sought
-- / RFI) is intentionally left without includePSC — early market-research
-- notices often carry no PSC yet, and its naics filter already scopes it.

-- 1. software PSC allow-list on every search except the Sources Sought / RFI one
UPDATE saved_search
SET query = jsonb_set(query, '{includePSC}', '["D","7A","7B","7J"]'::jsonb, true)
WHERE NOT (query ? 'includePSC')
  AND name <> 'B: Sources Sought / RFI — same NAICS';

-- 2. constrain the keyword-only firehose to the software naics set
UPDATE saved_search
SET query = jsonb_set(
      query,
      '{naics}',
      '["541511","541512","541513","541519","518210","513210"]'::jsonb,
      true
    )
WHERE name = 'C: DoD software factory keywords'
  AND NOT (query ? 'naics');
