package email

import "testing"

func TestSplitFrom(t *testing.T) {
	tests := []struct {
		name, in, wantHeader, wantEnvelope string
	}{
		{"bare address", "robot@junta-online.net", "robot@junta-online.net", "robot@junta-online.net"},
		{"display name", `"toldyouso robot" <robot@junta-online.net>`, `"toldyouso robot" <robot@junta-online.net>`, "robot@junta-online.net"},
		{"angle only", "<robot@junta-online.net>", "<robot@junta-online.net>", "robot@junta-online.net"},
		{"unparseable falls back", "not an address", "not an address", "not an address"},
		{"empty falls back", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, e := splitFrom(tc.in)
			if h != tc.wantHeader {
				t.Errorf("header: got %q, want %q", h, tc.wantHeader)
			}
			if e != tc.wantEnvelope {
				t.Errorf("envelope: got %q, want %q", e, tc.wantEnvelope)
			}
		})
	}
}
