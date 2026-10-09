// Package status defines the provider-agnostic status model rendered by the TUI.
//
// It intentionally contains no provider specifics: providers translate their own
// auth formats, endpoints, and quota semantics into this shared shape.
package status

import (
	"math"
	"strconv"
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
	// StateUnavailable means a supported source has no current data.
	StateUnavailable
)

// SourceQuality distinguishes a provider's own CLI from a first-party endpoint
// without a public contract, so the UI never presents a private API as if it
// were a supported one.
type SourceQuality int

const (
	// QualityCLI is the provider's own CLI as the source of truth.
	QualityCLI SourceQuality = iota
	// QualityPrivate is a first-party endpoint that is not a documented public API.
	QualityPrivate
)

func (q SourceQuality) String() string {
	switch q {
	case QualityCLI:
		return "provider CLI"
	case QualityPrivate:
		return "first-party (private endpoint)"
	default:
		return "unknown"
	}
}

// Window is a single quota/usage window within a provider. Percent is the
// used percentage; providers omit windows whose usage is unknown. A zero
// ResetsAt means the provider reported no reset time.
type Window struct {
	Label    string
	Percent  float64
	ResetsAt time.Time
}

// Provider is one provider's status.
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
}

// Snapshot is the state of every provider at one moment. CheckedAt is when the
// latest full refresh cycle completed; it is zero until the first one does.
// Refreshing reports that a cycle is still in flight.
type Snapshot struct {
	Providers  []Provider
	CheckedAt  time.Time
	Refreshing bool
}

// FormatPercent renders a used percentage to at most two decimals.
func FormatPercent(p float64) string {
	return strconv.FormatFloat(math.Round(p*100)/100, 'f', -1, 64) + "%"
}
