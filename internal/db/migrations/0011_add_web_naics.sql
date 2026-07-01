-- 0011_add_web_naics.sql — include website management / web-portal work.
--
-- Yield does website management, but search C's naics list only had the core
-- software codes. Web dev/hosting under 541511/541519/518210 was already caught,
-- but web-portal and web-publishing work often sits under 519290 (Web Search
-- Portals & All Other Information Services) or 516210 (media streaming / web
-- content). Add both so those notices are fetched; the includePSC allow-list
-- (group D = IT services, 7A/7B/7J = software) still keeps the result IT-only.
--
-- Idempotent: only extends the list when 519290 is not already present.
UPDATE saved_search
SET query = jsonb_set(
      query,
      '{naics}',
      '["541511","541512","541513","541519","518210","513210","519290","516210"]'::jsonb
    )
WHERE name = 'C: DoD software factory keywords'
  AND NOT (query->'naics' @> '["519290"]'::jsonb);
