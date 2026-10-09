package status

import (
	"strings"
	"testing"
)

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
		{-1, "?"},
		{-0.5, "?"},
	}
	for _, tt := range tests {
		if got := FormatPercent(tt.in); got != tt.want {
			t.Errorf("FormatPercent(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWindowKnown(t *testing.T) {
	if !(Window{Percent: 0}).Known() {
		t.Error("0% should be known")
	}
	if (Window{Percent: -1}).Known() {
		t.Error("-1 sentinel should be unknown")
	}
}

func TestSourceQualityStrings(t *testing.T) {
	cases := map[SourceQuality]string{
		QualityOfficial:   "official",
		QualityPrivate:    "first-party (private endpoint)",
		QualityCLI:        "provider CLI",
		QualityReverse:    "reverse-engineered",
		SourceQuality(99): "unknown",
	}
	for q, want := range cases {
		if got := q.String(); got != want {
			t.Errorf("SourceQuality(%d).String() = %q, want %q", q, got, want)
		}
	}
}

func TestSnapshotSortStableByName(t *testing.T) {
	s := Snapshot{Providers: []Provider{{Name: "OpenCode"}, {Name: "Claude"}, {Name: "Codex"}, {Name: "Gemini"}}}
	s.Sort()
	got := strings.Join([]string{s.Providers[0].Name, s.Providers[1].Name, s.Providers[2].Name, s.Providers[3].Name}, ",")
	if got != "Claude,Codex,Gemini,OpenCode" {
		t.Fatalf("sorted order = %q", got)
	}
}
