-- 0007_exclude_setaside.sql — add a client-side set-aside exclusion to every
-- saved search. SAM.gov's typeOfSetAside param is inclusion-only, so searches
-- that filter on NAICS alone (e.g. "B: Sources Sought / RFI") otherwise catch
-- set-asides Yield LLC can't win — most notably Buy Indian Act notices
-- (IEE / ISBEE), whose descriptions both contain "Indian".
--
-- The poller (poller.SavedSearchQuery.ExcludeSetAside) matches each rule as a
-- case-insensitive substring of typeOfSetAsideDescription. Unrestricted /
-- full-and-open notices carry no set-aside description and are never dropped.
--
-- Idempotent: only sets the key when it is absent, so re-running is a no-op and
-- any hand-tuned excludeSetAside list is left untouched.
UPDATE saved_search
SET query = jsonb_set(
      query,
      '{excludeSetAside}',
      '["Indian", "8(a)", "HUBZone", "Service-Disabled Veteran"]'::jsonb,
      true
    )
WHERE NOT (query ? 'excludeSetAside');
