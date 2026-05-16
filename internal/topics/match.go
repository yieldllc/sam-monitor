package topics

import "strings"

// matchKeywords does case-insensitive substring matching of `keywords` against
// `haystack`, returning the original-case copy of each keyword that matched.
// Order in the result follows the input keyword list (stable, deterministic).
//
// Used for client-side filtering of DSIP topics and as a second pass on
// SBIR.gov results where the upstream `keyword=` param only matches against
// solicitation title, not topic title or description.
func matchKeywords(haystack string, keywords []string) []string {
	if haystack == "" || len(keywords) == 0 {
		return nil
	}
	hay := strings.ToLower(haystack)
	var hits []string
	seen := make(map[string]bool, len(keywords))
	for _, kw := range keywords {
		k := strings.TrimSpace(kw)
		if k == "" || seen[strings.ToLower(k)] {
			continue
		}
		if strings.Contains(hay, strings.ToLower(k)) {
			hits = append(hits, k)
			seen[strings.ToLower(k)] = true
		}
	}
	return hits
}

// matchAny returns true if any keyword matches the haystack. Slightly cheaper
// than matchKeywords when only the boolean is needed.
func matchAny(haystack string, keywords []string) bool {
	if haystack == "" || len(keywords) == 0 {
		return false
	}
	hay := strings.ToLower(haystack)
	for _, kw := range keywords {
		k := strings.TrimSpace(kw)
		if k == "" {
			continue
		}
		if strings.Contains(hay, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
