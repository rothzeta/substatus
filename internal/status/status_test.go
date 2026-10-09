package status

import "testing"

func TestFormatPercent(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0%"},
		{8, "8%"},
		{28, "28%"},
		{0.9499, "0.95%"},
		{29.1208, "29.12%"},
		{99.996, "100%"},
		{100, "100%"},
		{112.5, "112.5%"},
	}
	for _, tt := range tests {
		if got := FormatPercent(tt.in); got != tt.want {
			t.Errorf("FormatPercent(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSourceQualityStrings(t *testing.T) {
	cases := map[SourceQuality]string{
		QualityPrivate:    "first-party (private endpoint)",
		QualityCLI:        "provider CLI",
		SourceQuality(99): "unknown",
	}
	for q, want := range cases {
		if got := q.String(); got != want {
			t.Errorf("SourceQuality(%d).String() = %q, want %q", q, got, want)
		}
	}
}
