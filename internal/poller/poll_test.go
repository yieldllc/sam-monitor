package poller

import "testing"

func TestSetAsideExcluded(t *testing.T) {
	// Excludes are matched case-insensitively as substrings against SAM.gov's
	// typeOfSetAsideDescription. A single "Indian" rule covers both the
	// Indian Economic Enterprise (IEE) and Indian Small Business Economic
	// Enterprise (ISBEE) Buy Indian Act descriptions.
	excludes := []string{"Indian", "8(a)", "HUBZone", "Service-Disabled Veteran"}

	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"empty description never excluded", "", false},
		{"unrestricted (no set-aside) kept", "", false},
		{
			"ISBEE excluded",
			"Indian Small Business Economic Enterprise (ISBEE)",
			true,
		},
		{
			"IEE excluded by same Indian rule",
			"Indian Economic Enterprise (IEE)",
			true,
		},
		{"case-insensitive match", "indian economic enterprise", true},
		{"8(a) excluded", "8(a) Set-Aside (FAR 19.8)", true},
		{"HUBZone excluded", "Historically Underutilized Business (HUBZone) Set-Aside", true},
		{"SDVOSB excluded", "Service-Disabled Veteran-Owned Small Business (SDVOSB) Set-Aside", true},
		{"Total Small Business kept", "Total Small Business Set-Aside (FAR 19.5)", false},
		{"WOSB kept", "Women-Owned Small Business (WOSB) Program Set-Aside", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := setAsideExcluded(tt.desc, excludes); got != tt.want {
				t.Errorf("setAsideExcluded(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestSetAsideExcluded_NoRules(t *testing.T) {
	// With no exclude rules configured, nothing is filtered.
	if setAsideExcluded("Indian Small Business Economic Enterprise (ISBEE)", nil) {
		t.Error("no exclude rules should never exclude")
	}
}
