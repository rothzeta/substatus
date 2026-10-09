// Package status defines the provider-agnostic status model rendered by the TUI.
//
// It intentionally contains no provider specifics: providers translate their own
// auth formats, endpoints, and quota semantics into this shared shape.
package status

import (
	"sort"
	"time"
)

// State is the coarse lifecycle state of a provider check. Per-provider errors
// never mask successful providers: every provider reports exactly one State.
type State int

const (
	// StateLoading means a check is queued or in flight.
	StateLoading State = iota
	// StateOK means usage data was retrieved.
	StateOK
	// StateAuthMissing means the CLI/credentials needed for a source aren't present.
	StateAuthMissing
	// StateNotInstalled means the provider CLI or config was not found on this host.
	StateNotInstalled
	// StateUnsupported means the provider is present but exposes no stable usage source.
	StateUnsupported
	// StateError means a check ran and failed (network, HTTP, parse).
	StateError
	// StateUnavailable means a supported source needs setup or has no current data.
	StateUnavailable
)

// SourceQuality distinguishes documented/provider-first-party sources from
// reverse-engineered or private ones, so the UI never presents a private API as
// if it were a supported contract.
type SourceQuality int

const (
	// QualityOfficial is a documented, provider-first-party interface.
	QualityOfficial SourceQuality = iota
	// QualityPrivate is a first-party endpoint that is not a documented public API.
	QualityPrivate
	// QualityCLI is the provider's own CLI as the source of truth.
	QualityCLI
	// QualityReverse is a reverse-engineered or undocumented third-party endpoint.
	QualityReverse
)

func (q SourceQuality) String() string {
	switch q {
	case QualityOfficial:
		return "official"
	case QualityPrivate:
		return "first-party (private endpoint)"
	case QualityCLI:
		return "provider CLI"
	case QualityReverse:
		return "reverse-engineered"
	default:
		return "unknown"
	}
}

// Window is a single quota/usage window within a provider. Percent is
// "used" percentage in [0,100] when known; -1 means unknown.
type Window struct {
	Label    string
	Percent  float64
	ResetsAt time.Time
	HasReset bool
}

// Known reports whether the window carries a percentage.
func (w Window) Known() bool { return w.Percent >= 0 }

// Provider is one provider's status snapshot.
type Provider struct {
	Name    string
	State   State
	Plan    string
	Windows []Window
	// Note is a short, redacted, human-readable detail: limitation, source
	// provenance, or error summary. It never contains credentials.
	Note string
	// Source names the endpoint or CLI the data came from (no secrets).
	Source  string
	Quality SourceQuality
	// Err is the redacted error string when State == StateError.
	Err     string
	Checked time.Time
}

// Snapshot is the full result of one refresh cycle.
type Snapshot struct {
	Providers []Provider
	CheckedAt time.Time
}

// Sort orders providers by name for stable rendering.
func (s *Snapshot) Sort() {
	sort.Slice(s.Providers, func(i, j int) bool { return s.Providers[i].Name < s.Providers[j].Name })
}

// FormatPercent renders a used percentage, handling the unknown sentinel.
func FormatPercent(p float64) string {
	if p < 0 {
		return "?"
	}
	return trimFloat(p) + "%"
}

func trimFloat(f float64) string {
	s := strconvFormat(f)
	return s
}

// strconvFormat avoids pulling fmt into hot paths of callers that only need a
// single number; kept separate so tests can exercise rounding behavior.
func strconvFormat(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f)
	frac := int64((f-float64(whole))*100 + 0.5)
	if frac >= 100 {
		whole++
		frac -= 100
	}
	out := itoa(whole)
	if frac != 0 {
		out += "." + pad2(frac)
	}
	if neg {
		out = "-" + out
	}
	return out
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func pad2(v int64) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}
