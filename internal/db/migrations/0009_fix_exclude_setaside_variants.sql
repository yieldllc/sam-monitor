-- 0009_fix_exclude_setaside_variants.sql — broaden the set-aside exclusion
-- substrings to cover SAM.gov's inconsistent spellings.
--
-- Migration 0007 seeded ["Indian", "8(a)", "HUBZone", "Service-Disabled Veteran"],
-- but SAM.gov writes the same set-aside several ways:
--   "8(a) Sole Source"  vs  "8a Competed"                    → need "8(a)" AND "8a "
--   "Service-Disabled Veteran-Owned..."  vs  "SDVOSB Sole Source" → need "SDVOSB"
-- The original list silently let "8a Competed" and "SDVOSB Sole Source" through.
--
-- The "8a " rule keeps a trailing space so it matches the "8a Competed" form
-- without risking false hits on unrelated tokens. Overwrites unconditionally so
-- a fresh DB (which ran 0007 first) converges to the complete list.
UPDATE saved_search
SET query = jsonb_set(
      query,
      '{excludeSetAside}',
      '["Indian", "8(a)", "8a ", "HUBZone", "SDVOSB", "Service-Disabled Veteran"]'::jsonb
    )
WHERE query ? 'excludeSetAside';
