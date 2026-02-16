package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/session"
)

func setupNudgeTestRegistry(t *testing.T) {
	t.Helper()
	reg := session.NewPrefixRegistry()
	reg.Register("gt", "gastown")
	reg.Register("bd", "beads")
	old := session.DefaultRegistry()
	session.SetDefaultRegistry(reg)
	t.Cleanup(func() { session.SetDefaultRegistry(old) })
}

func TestNudgeStdinConflict(t *testing.T) {
	// Save and restore package-level flags
	origMessage := nudgeMessageFlag
	origStdin := nudgeStdinFlag
	defer func() {
		nudgeMessageFlag = origMessage
		nudgeStdinFlag = origStdin
	}()

	// When both --stdin and --message are set, runNudge should return an error
	nudgeStdinFlag = true
	nudgeMessageFlag = "some message"

	err := runNudge(nudgeCmd, []string{"gastown/alpha"})
	if err == nil {
		t.Fatal("expected error when --stdin and --message are both set")
	}
	if !strings.Contains(err.Error(), "cannot use --stdin with --message/-m") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveNudgePattern(t *testing.T) {
	setupNudgeTestRegistry(t)
	// Create test agent sessions (using rig prefixes)
	agents := []*AgentSession{
		{Name: "hq-mayor", Type: AgentMayor},
		{Name: "hq-deacon", Type: AgentDeacon},
		{Name: "gt-witness", Type: AgentWitness, Rig: "gastown"},
		{Name: "gt-refinery", Type: AgentRefinery, Rig: "gastown"},
		{Name: "gt-crew-max", Type: AgentCrew, Rig: "gastown", AgentName: "max"},
		{Name: "gt-crew-jack", Type: AgentCrew, Rig: "gastown", AgentName: "jack"},
		{Name: "gt-alpha", Type: AgentPolecat, Rig: "gastown", AgentName: "alpha"},
		{Name: "gt-beta", Type: AgentPolecat, Rig: "gastown", AgentName: "beta"},
		{Name: "bd-witness", Type: AgentWitness, Rig: "beads"},
		{Name: "bd-gamma", Type: AgentPolecat, Rig: "beads", AgentName: "gamma"},
	}

	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "mayor special case",
			pattern:  "mayor",
			expected: []string{"hq-mayor"},
		},
		{
			name:     "deacon special case",
			pattern:  "deacon",
			expected: []string{"hq-deacon"},
		},
		{
			name:     "specific witness",
			pattern:  "gastown/witness",
			expected: []string{"gt-witness"},
		},
		{
			name:     "all witnesses",
			pattern:  "*/witness",
			expected: []string{"gt-witness", "bd-witness"},
		},
		{
			name:     "specific refinery",
			pattern:  "gastown/refinery",
			expected: []string{"gt-refinery"},
		},
		{
			name:     "all polecats in rig",
			pattern:  "gastown/polecats/*",
			expected: []string{"gt-alpha", "gt-beta"},
		},
		{
			name:     "specific polecat",
			pattern:  "gastown/polecats/alpha",
			expected: []string{"gt-alpha"},
		},
		{
			name:     "all crew in rig",
			pattern:  "gastown/crew/*",
			expected: []string{"gt-crew-max", "gt-crew-jack"},
		},
		{
			name:     "specific crew member",
			pattern:  "gastown/crew/max",
			expected: []string{"gt-crew-max"},
		},
		{
			name:     "legacy polecat format",
			pattern:  "gastown/alpha",
			expected: []string{"gt-alpha"},
		},
		{
			name:     "no matches",
			pattern:  "nonexistent/polecats/*",
			expected: nil,
		},
		{
			name:     "invalid pattern",
			pattern:  "invalid",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNudgePattern(tt.pattern, agents)

			if len(got) != len(tt.expected) {
				t.Errorf("resolveNudgePattern(%q) returned %d results, want %d: got %v, want %v",
					tt.pattern, len(got), len(tt.expected), got, tt.expected)
				return
			}

			// Check each expected value is present
			gotMap := make(map[string]bool)
			for _, g := range got {
				gotMap[g] = true
			}
			for _, e := range tt.expected {
				if !gotMap[e] {
					t.Errorf("resolveNudgePattern(%q) missing expected %q, got %v",
						tt.pattern, e, got)
				}
			}
		})
	}
}

func TestSessionNameToAddress(t *testing.T) {
	setupNudgeTestRegistry(t)
	tests := []struct {
		name        string
		sessionName string
		expected    string
	}{
		{
			name:        "mayor",
			sessionName: "hq-mayor",
			expected:    "mayor",
		},
		{
			name:        "deacon",
			sessionName: "hq-deacon",
			expected:    "deacon",
		},
		{
			name:        "witness",
			sessionName: "gt-witness",
			expected:    "gastown/witness",
		},
		{
			name:        "refinery",
			sessionName: "gt-refinery",
			expected:    "gastown/refinery",
		},
		{
			name:        "crew member",
			sessionName: "gt-crew-max",
			expected:    "gastown/crew/max",
		},
		{
			name:        "polecat",
			sessionName: "gt-alpha",
			expected:    "gastown/alpha",
		},
		{
			name:        "unrecognized format",
			sessionName: "plaintext",
			expected:    "",
		},
		{
			name:        "gt prefix but no name",
			sessionName: "gt-",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionNameToAddress(tt.sessionName)
			if got != tt.expected {
				t.Errorf("sessionNameToAddress(%q) = %q, want %q", tt.sessionName, got, tt.expected)
			}
		})
	}
}

func TestIfFreshMaxAge(t *testing.T) {
	// Verify the constant is 60 seconds as specified in the design.
	if ifFreshMaxAge != 60*time.Second {
		t.Errorf("ifFreshMaxAge = %v, want 60s", ifFreshMaxAge)
	}
}

func TestIfFreshSessionAgeCheck(t *testing.T) {
	// Test the age comparison logic used by --if-fresh.
	// A session created 10 seconds ago should be "fresh" (nudge allowed).
	// A session created 120 seconds ago should be "stale" (nudge suppressed).
	now := time.Now()

	tests := []struct {
		name        string
		createdAt   time.Time
		shouldNudge bool
	}{
		{
			name:        "fresh session (10s old)",
			createdAt:   now.Add(-10 * time.Second),
			shouldNudge: true,
		},
		{
			name:        "borderline session (59s old)",
			createdAt:   now.Add(-59 * time.Second),
			shouldNudge: true,
		},
		{
			name:        "stale session (61s old)",
			createdAt:   now.Add(-61 * time.Second),
			shouldNudge: false,
		},
		{
			name:        "very stale session (5min old)",
			createdAt:   now.Add(-5 * time.Minute),
			shouldNudge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age := time.Since(tt.createdAt)
			shouldNudge := age <= ifFreshMaxAge
			if shouldNudge != tt.shouldNudge {
				t.Errorf("age=%v: shouldNudge=%v, want %v", age, shouldNudge, tt.shouldNudge)
			}
		})
	}
}
