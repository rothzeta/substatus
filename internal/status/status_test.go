package status

import "testing"

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
