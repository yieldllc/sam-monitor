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

func TestPscIncluded(t *testing.T) {
	// Software allow-list: IT services (D*) + software/security products (7A/7B/7J).
	allow := []string{"D", "7A", "7B", "7J"}

	tests := []struct {
		name string
		code string
		want bool
	}{
		{"IT services DA10 kept", "DA10", true},
		{"IT services D302 kept", "D302", true},
		{"business app software 7A21 kept", "7A21", true},
		{"system software 7B20 kept", "7B20", true},
		{"IT security 7J20 kept", "7J20", true},
		{"lowercase code still matches", "da01", true},
		{"HVAC refrigeration PSC 41 dropped", "41", false},
		{"air-conditioning 4120 dropped", "4120", false},
		{"medical 6525 dropped", "6525", false},
		{"IT hardware 7E (servers) dropped by this list", "7E20", false},
		{"empty code dropped when allow-list set", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pscIncluded(tt.code, allow); got != tt.want {
				t.Errorf("pscIncluded(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestPscIncluded_NoAllowList(t *testing.T) {
	// With no allow-list, every code (even empty) passes through.
	if !pscIncluded("41", nil) {
		t.Error("empty allow-list should include everything")
	}
	if !pscIncluded("", nil) {
		t.Error("empty allow-list should include empty code too")
	}
}
