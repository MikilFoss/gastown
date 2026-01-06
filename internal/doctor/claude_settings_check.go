package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/claude"
	"github.com/steveyegge/gastown/internal/tmux"
)

// ClaudeSettingsCheck verifies that Claude settings.json files match the expected templates.
// Detects stale settings files that are missing required hooks or configuration.
type ClaudeSettingsCheck struct {
	FixableCheck
	staleSettings []staleSettingsInfo
}

type staleSettingsInfo struct {
	path          string   // Full path to settings.json
	agentType     string   // e.g., "witness", "refinery", "deacon", "mayor"
	rigName       string   // Rig name (empty for town-level agents)
	sessionName   string   // tmux session name for cycling
	missing       []string // What's missing from the settings
	wrongLocation bool     // True if file is in wrong location (should be deleted)
}

// NewClaudeSettingsCheck creates a new Claude settings validation check.
func NewClaudeSettingsCheck() *ClaudeSettingsCheck {
	return &ClaudeSettingsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "claude-settings",
				CheckDescription: "Verify Claude settings.json files match expected templates",
			},
		},
	}
}

// Run checks all Claude settings.json files for staleness.
func (c *ClaudeSettingsCheck) Run(ctx *CheckContext) *CheckResult {
	c.staleSettings = nil

	var details []string

	// Find all settings.json files
	settingsFiles := c.findSettingsFiles(ctx.TownRoot)

	for _, sf := range settingsFiles {
		// Files in wrong locations are always stale (should be deleted)
		if sf.wrongLocation {
			c.staleSettings = append(c.staleSettings, sf)
			details = append(details, fmt.Sprintf("%s: wrong location (should be in rig/ subdirectory)", sf.path))
			continue
		}

		// Check content of files in correct locations
		missing := c.checkSettings(sf.path, sf.agentType)
		if len(missing) > 0 {
			sf.missing = missing
			c.staleSettings = append(c.staleSettings, sf)
			details = append(details, fmt.Sprintf("%s: missing %s", sf.path, strings.Join(missing, ", ")))
		}
	}

	if len(c.staleSettings) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All Claude settings.json files are up to date",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Found %d stale Claude settings.json file(s)", len(c.staleSettings)),
		Details: details,
		FixHint: "Run 'gt doctor --fix' to update settings and restart affected agents",
	}
}

// findSettingsFiles locates all .claude/settings.json files and identifies their agent type.
func (c *ClaudeSettingsCheck) findSettingsFiles(townRoot string) []staleSettingsInfo {
	var files []staleSettingsInfo

	// Town-level: mayor (~/gt/.claude/settings.json)
	mayorSettings := filepath.Join(townRoot, ".claude", "settings.json")
	if fileExists(mayorSettings) {
		files = append(files, staleSettingsInfo{
			path:        mayorSettings,
			agentType:   "mayor",
			sessionName: "gt-mayor",
		})
	}

	// Town-level: deacon (~/gt/deacon/.claude/settings.json)
	deaconSettings := filepath.Join(townRoot, "deacon", ".claude", "settings.json")
	if fileExists(deaconSettings) {
		files = append(files, staleSettingsInfo{
			path:        deaconSettings,
			agentType:   "deacon",
			sessionName: "gt-deacon",
		})
	}

	// Find rig directories
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return files
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		rigName := entry.Name()
		rigPath := filepath.Join(townRoot, rigName)

		// Skip known non-rig directories
		if rigName == "mayor" || rigName == "deacon" || rigName == "daemon" ||
			rigName == ".git" || rigName == "docs" || rigName[0] == '.' {
			continue
		}

		// Check for witness settings - rig/ is correct location, without rig/ is wrong
		witnessRigSettings := filepath.Join(rigPath, "witness", "rig", ".claude", "settings.json")
		if fileExists(witnessRigSettings) {
			files = append(files, staleSettingsInfo{
				path:        witnessRigSettings,
				agentType:   "witness",
				rigName:     rigName,
				sessionName: fmt.Sprintf("gt-%s-witness", rigName),
			})
		}
		// Settings in witness/.claude/ (not witness/rig/.claude/) are in wrong location
		witnessWrongSettings := filepath.Join(rigPath, "witness", ".claude", "settings.json")
		if fileExists(witnessWrongSettings) {
			files = append(files, staleSettingsInfo{
				path:          witnessWrongSettings,
				agentType:     "witness",
				rigName:       rigName,
				sessionName:   fmt.Sprintf("gt-%s-witness", rigName),
				wrongLocation: true,
			})
		}

		// Check for refinery settings - rig/ is correct location, without rig/ is wrong
		refineryRigSettings := filepath.Join(rigPath, "refinery", "rig", ".claude", "settings.json")
		if fileExists(refineryRigSettings) {
			files = append(files, staleSettingsInfo{
				path:        refineryRigSettings,
				agentType:   "refinery",
				rigName:     rigName,
				sessionName: fmt.Sprintf("gt-%s-refinery", rigName),
			})
		}
		// Settings in refinery/.claude/ (not refinery/rig/.claude/) are in wrong location
		refineryWrongSettings := filepath.Join(rigPath, "refinery", ".claude", "settings.json")
		if fileExists(refineryWrongSettings) {
			files = append(files, staleSettingsInfo{
				path:          refineryWrongSettings,
				agentType:     "refinery",
				rigName:       rigName,
				sessionName:   fmt.Sprintf("gt-%s-refinery", rigName),
				wrongLocation: true,
			})
		}

		// Check for crew settings (crew/<name>/.claude/)
		crewDir := filepath.Join(rigPath, "crew")
		if dirExists(crewDir) {
			crewEntries, _ := os.ReadDir(crewDir)
			for _, crewEntry := range crewEntries {
				if !crewEntry.IsDir() {
					continue
				}
				crewSettings := filepath.Join(crewDir, crewEntry.Name(), ".claude", "settings.json")
				if fileExists(crewSettings) {
					files = append(files, staleSettingsInfo{
						path:        crewSettings,
						agentType:   "crew",
						rigName:     rigName,
						sessionName: fmt.Sprintf("gt-%s-crew-%s", rigName, crewEntry.Name()),
					})
				}
			}
		}

		// Check for polecat settings (polecats/<name>/.claude/)
		polecatsDir := filepath.Join(rigPath, "polecats")
		if dirExists(polecatsDir) {
			polecatEntries, _ := os.ReadDir(polecatsDir)
			for _, pcEntry := range polecatEntries {
				if !pcEntry.IsDir() {
					continue
				}
				pcSettings := filepath.Join(polecatsDir, pcEntry.Name(), ".claude", "settings.json")
				if fileExists(pcSettings) {
					files = append(files, staleSettingsInfo{
						path:        pcSettings,
						agentType:   "polecat",
						rigName:     rigName,
						sessionName: fmt.Sprintf("gt-%s-polecat-%s", rigName, pcEntry.Name()),
					})
				}
			}
		}
	}

	return files
}

// checkSettings compares a settings file against the expected template.
// Returns a list of what's missing.
func (c *ClaudeSettingsCheck) checkSettings(path, agentType string) []string {
	var missing []string

	// Read the actual settings
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"unreadable"}
	}

	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		return []string{"invalid JSON"}
	}

	// Check for required elements based on template
	// All templates should have:
	// 1. enabledPlugins
	// 2. PATH export in hooks
	// 3. Stop hook with gt costs record (for autonomous)
	// 4. gt nudge deacon session-started in SessionStart

	// Check enabledPlugins
	if _, ok := actual["enabledPlugins"]; !ok {
		missing = append(missing, "enabledPlugins")
	}

	// Check hooks
	hooks, ok := actual["hooks"].(map[string]any)
	if !ok {
		return append(missing, "hooks")
	}

	// Check SessionStart hook has PATH export
	if !c.hookHasPattern(hooks, "SessionStart", "PATH=") {
		missing = append(missing, "PATH export")
	}

	// Check SessionStart hook has deacon nudge
	if !c.hookHasPattern(hooks, "SessionStart", "gt nudge deacon session-started") {
		missing = append(missing, "deacon nudge")
	}

	// Check Stop hook exists with gt costs record (for all roles)
	if !c.hookHasPattern(hooks, "Stop", "gt costs record") {
		missing = append(missing, "Stop hook")
	}

	return missing
}

// hookHasPattern checks if a hook contains a specific pattern.
func (c *ClaudeSettingsCheck) hookHasPattern(hooks map[string]any, hookName, pattern string) bool {
	hookList, ok := hooks[hookName].([]any)
	if !ok {
		return false
	}

	for _, hook := range hookList {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		innerHooks, ok := hookMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, inner := range innerHooks {
			innerMap, ok := inner.(map[string]any)
			if !ok {
				continue
			}
			cmd, ok := innerMap["command"].(string)
			if ok && strings.Contains(cmd, pattern) {
				return true
			}
		}
	}
	return false
}

// Fix deletes stale settings files and restarts affected agents.
func (c *ClaudeSettingsCheck) Fix(ctx *CheckContext) error {
	var errors []string
	t := tmux.NewTmux()

	for _, sf := range c.staleSettings {
		// Delete the stale settings file
		if err := os.Remove(sf.path); err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete %s: %v", sf.path, err))
			continue
		}

		// Also delete parent .claude directory if empty
		claudeDir := filepath.Dir(sf.path)
		_ = os.Remove(claudeDir) // Best-effort, will fail if not empty

		// For files in wrong locations, just delete - don't recreate
		// The correct location will get settings when the agent starts
		if sf.wrongLocation {
			continue
		}

		// Recreate settings using EnsureSettingsForRole
		workDir := filepath.Dir(claudeDir) // agent work directory
		if err := claude.EnsureSettingsForRole(workDir, sf.agentType); err != nil {
			errors = append(errors, fmt.Sprintf("failed to recreate settings for %s: %v", sf.path, err))
			continue
		}

		// Check if agent has a running session
		running, _ := t.HasSession(sf.sessionName)
		if running {
			// Cycle the agent by killing and letting gt up restart it
			// (or the daemon will restart it)
			_ = t.KillSession(sf.sessionName)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
