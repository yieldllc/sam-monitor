package topics

import (
	"reflect"
	"testing"
)

func TestMatchKeywords(t *testing.T) {
	keywords := []string{
		"container", "hardened image", "SBOM", "supply chain",
		"provenance", "software supply chain", "Iron Bank",
		"Platform One", "reproducible build", "attestation", "cATO",
	}

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{
			"single-hit case-insensitive",
			"This solicitation involves Container security.",
			[]string{"container"},
		},
		{
			"multi-hit preserves keyword order",
			"Iron Bank hardened image with SBOM and attestation generation.",
			[]string{"hardened image", "SBOM", "Iron Bank", "attestation"},
		},
		{
			"acronym cATO matches the upper-case form",
			"continuous ATO (cATO) pipeline",
			[]string{"cATO"},
		},
		{
			"no false positive on substring of unrelated word",
			"this is about quantum computing and laser optics",
			nil,
		},
		{
			"phrase substring across boundaries",
			"end-to-end SOFTWARE SUPPLY CHAIN security",
			// 'supply chain' is a substring of 'software supply chain' so both
			// match. Order follows the keyword input order.
			[]string{"supply chain", "software supply chain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchKeywords(tt.in, keywords)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("matchKeywords(%q)\n  got  %v\n  want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestMatchAny(t *testing.T) {
	kw := []string{"container", "SBOM"}
	if !matchAny("a story about SBOM tooling", kw) {
		t.Error("expected match on SBOM")
	}
	if matchAny("a story about quantum optics", kw) {
		t.Error("expected no match")
	}
	if matchAny("", kw) {
		t.Error("empty haystack should not match")
	}
}
