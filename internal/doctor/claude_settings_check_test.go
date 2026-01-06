package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClaudeSettingsCheck(t *testing.T) {
	check := NewClaudeSettingsCheck()

	if check.Name() != "claude-settings" {
		t.Errorf("expected name 'claude-settings', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestClaudeSettingsCheck_NoSettingsFiles(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no settings files, got %v", result.Status)
	}
}

// createValidSettings creates a valid settings.json with all required elements.
func createValidSettings(t *testing.T, path string) {
	t.Helper()

	settings := map[string]any{
		"enabledPlugins": []string{"plugin1"},
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "**",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "export PATH=/usr/local/bin:$PATH",
						},
						map[string]any{
							"type":    "command",
							"command": "gt nudge deacon session-started",
						},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"matcher": "**",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "gt costs record --session $CLAUDE_SESSION_ID",
						},
					},
				},
			},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// createStaleSettings creates a settings.json missing required elements.
func createStaleSettings(t *testing.T, path string, missingElements ...string) {
	t.Helper()

	settings := map[string]any{
		"enabledPlugins": []string{"plugin1"},
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "**",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "export PATH=/usr/local/bin:$PATH",
						},
						map[string]any{
							"type":    "command",
							"command": "gt nudge deacon session-started",
						},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"matcher": "**",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "gt costs record --session $CLAUDE_SESSION_ID",
						},
					},
				},
			},
		},
	}

	for _, missing := range missingElements {
		switch missing {
		case "enabledPlugins":
			delete(settings, "enabledPlugins")
		case "hooks":
			delete(settings, "hooks")
		case "PATH":
			// Remove PATH from SessionStart hooks
			hooks := settings["hooks"].(map[string]any)
			sessionStart := hooks["SessionStart"].([]any)
			hookObj := sessionStart[0].(map[string]any)
			innerHooks := hookObj["hooks"].([]any)
			// Filter out PATH command
			var filtered []any
			for _, h := range innerHooks {
				hMap := h.(map[string]any)
				if cmd, ok := hMap["command"].(string); ok && !strings.Contains(cmd, "PATH=") {
					filtered = append(filtered, h)
				}
			}
			hookObj["hooks"] = filtered
		case "deacon-nudge":
			// Remove deacon nudge from SessionStart hooks
			hooks := settings["hooks"].(map[string]any)
			sessionStart := hooks["SessionStart"].([]any)
			hookObj := sessionStart[0].(map[string]any)
			innerHooks := hookObj["hooks"].([]any)
			// Filter out deacon nudge
			var filtered []any
			for _, h := range innerHooks {
				hMap := h.(map[string]any)
				if cmd, ok := hMap["command"].(string); ok && !strings.Contains(cmd, "gt nudge deacon") {
					filtered = append(filtered, h)
				}
			}
			hookObj["hooks"] = filtered
		case "Stop":
			hooks := settings["hooks"].(map[string]any)
			delete(hooks, "Stop")
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeSettingsCheck_ValidMayorSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid mayor settings
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createValidSettings(t, mayorSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_ValidDeaconSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid deacon settings
	deaconSettings := filepath.Join(tmpDir, "deacon", ".claude", "settings.json")
	createValidSettings(t, deaconSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid deacon settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_ValidWitnessSettings(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create valid witness settings in correct location (rig/.claude/)
	witnessSettings := filepath.Join(tmpDir, rigName, "witness", "rig", ".claude", "settings.json")
	createValidSettings(t, witnessSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid witness settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_ValidRefinerySettings(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create valid refinery settings in correct location
	refinerySettings := filepath.Join(tmpDir, rigName, "refinery", "rig", ".claude", "settings.json")
	createValidSettings(t, refinerySettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid refinery settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_ValidCrewSettings(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create valid crew agent settings
	crewSettings := filepath.Join(tmpDir, rigName, "crew", "agent1", ".claude", "settings.json")
	createValidSettings(t, crewSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid crew settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_ValidPolecatSettings(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create valid polecat settings
	pcSettings := filepath.Join(tmpDir, rigName, "polecats", "pc1", ".claude", "settings.json")
	createValidSettings(t, pcSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid polecat settings, got %v: %s", result.Status, result.Message)
	}
}

func TestClaudeSettingsCheck_MissingEnabledPlugins(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale mayor settings missing enabledPlugins
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "enabledPlugins")

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing enabledPlugins, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "1 stale") {
		t.Errorf("expected message about stale settings, got %q", result.Message)
	}
}

func TestClaudeSettingsCheck_MissingHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale settings missing hooks entirely
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "hooks")

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing hooks, got %v", result.Status)
	}
}

func TestClaudeSettingsCheck_MissingPATH(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale settings missing PATH export
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "PATH")

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing PATH, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "PATH export") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention PATH export, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_MissingDeaconNudge(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale settings missing deacon nudge
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "deacon-nudge")

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing deacon nudge, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "deacon nudge") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention deacon nudge, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_MissingStopHook(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale settings missing Stop hook
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "Stop")

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing Stop hook, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "Stop hook") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention Stop hook, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_WrongLocationWitness(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create settings in wrong location (witness/.claude/ instead of witness/rig/.claude/)
	wrongSettings := filepath.Join(tmpDir, rigName, "witness", ".claude", "settings.json")
	createValidSettings(t, wrongSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for wrong location, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "wrong location") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention wrong location, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_WrongLocationRefinery(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create settings in wrong location (refinery/.claude/ instead of refinery/rig/.claude/)
	wrongSettings := filepath.Join(tmpDir, rigName, "refinery", ".claude", "settings.json")
	createValidSettings(t, wrongSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for wrong location, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "wrong location") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention wrong location, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_MultipleStaleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create multiple stale settings files
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createStaleSettings(t, mayorSettings, "PATH")

	deaconSettings := filepath.Join(tmpDir, "deacon", ".claude", "settings.json")
	createStaleSettings(t, deaconSettings, "Stop")

	witnessWrong := filepath.Join(tmpDir, rigName, "witness", ".claude", "settings.json")
	createValidSettings(t, witnessWrong) // Valid content but wrong location

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for multiple stale files, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "3 stale") {
		t.Errorf("expected message about 3 stale files, got %q", result.Message)
	}
}

func TestClaudeSettingsCheck_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid JSON file
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(mayorSettings), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mayorSettings, []byte("not valid json {"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for invalid JSON, got %v", result.Status)
	}
	found := false
	for _, d := range result.Details {
		if strings.Contains(d, "invalid JSON") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected details to mention invalid JSON, got %v", result.Details)
	}
}

func TestClaudeSettingsCheck_FixDeletesStaleFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create stale settings in wrong location (easy to test - just delete, no recreate)
	rigName := "testrig"
	wrongSettings := filepath.Join(tmpDir, rigName, "witness", ".claude", "settings.json")
	createValidSettings(t, wrongSettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	// Run to detect
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(wrongSettings); !os.IsNotExist(err) {
		t.Error("expected wrong location settings to be deleted")
	}

	// Verify check passes (no settings files means OK)
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestClaudeSettingsCheck_SkipsNonRigDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directories that should be skipped
	for _, skipDir := range []string{"mayor", "deacon", "daemon", ".git", "docs", ".hidden"} {
		dir := filepath.Join(tmpDir, skipDir, "witness", "rig", ".claude")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		// These should NOT be detected as rig witness settings
		settingsPath := filepath.Join(dir, "settings.json")
		createStaleSettings(t, settingsPath, "PATH")
	}

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	_ = check.Run(ctx)

	// Should only find mayor and deacon settings in their specific locations
	// The witness settings in these dirs should be ignored
	// Since we didn't create valid mayor/deacon settings, those will be stale
	// But the ones in "mayor/witness/rig/.claude" should be ignored

	// Count how many stale files were found - should be 0 since none of the
	// skipped directories have their settings detected
	if len(check.staleSettings) != 0 {
		t.Errorf("expected 0 stale files (skipped dirs), got %d", len(check.staleSettings))
	}
}

func TestClaudeSettingsCheck_MixedValidAndStale(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create valid mayor settings
	mayorSettings := filepath.Join(tmpDir, ".claude", "settings.json")
	createValidSettings(t, mayorSettings)

	// Create stale witness settings (missing PATH)
	witnessSettings := filepath.Join(tmpDir, rigName, "witness", "rig", ".claude", "settings.json")
	createStaleSettings(t, witnessSettings, "PATH")

	// Create valid refinery settings
	refinerySettings := filepath.Join(tmpDir, rigName, "refinery", "rig", ".claude", "settings.json")
	createValidSettings(t, refinerySettings)

	check := NewClaudeSettingsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for mixed valid/stale, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "1 stale") {
		t.Errorf("expected message about 1 stale file, got %q", result.Message)
	}
	// Should only report the witness settings as stale
	if len(result.Details) != 1 {
		t.Errorf("expected 1 detail, got %d: %v", len(result.Details), result.Details)
	}
}
