package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Verify skill file exists.
	skill, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "mycelium", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill file not created: %v", err)
	}
	if !strings.Contains(string(skill), "name: mycelium") {
		t.Error("skill file missing frontmatter")
	}
	if !strings.Contains(string(skill), "Bash(mctl *)") {
		t.Error("skill file missing default mctl-path in allowed-tools")
	}

	// Verify settings.json exists and has hook.
	settingsBytes, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings.json missing hooks")
	}
	entries, ok := hooks["UserPromptSubmit"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatal("settings.json missing UserPromptSubmit hooks")
	}

	// Verify .mcp.json exists and has mycelium server.
	mcpBytes, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json not created: %v", err)
	}
	var mcp map[string]any
	if err := json.Unmarshal(mcpBytes, &mcp); err != nil {
		t.Fatalf(".mcp.json not valid JSON: %v", err)
	}
	servers, ok := mcp["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json missing mcpServers")
	}
	mycelium, ok := servers["mycelium"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json missing mycelium server entry")
	}
	if mycelium["command"] != "mctl" {
		t.Errorf("expected command 'mctl', got %q", mycelium["command"])
	}
}

func TestSetupIdempotent(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Run setup twice.
	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first setup failed: %v", err)
	}

	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second setup failed: %v", err)
	}

	// Verify no duplicate hooks.
	settingsBytes, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings map[string]any
	json.Unmarshal(settingsBytes, &settings)
	hooks := settings["hooks"].(map[string]any)
	entries := hooks["UserPromptSubmit"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 hook entry after double setup, got %d", len(entries))
	}

	// Verify .mcp.json has only one mycelium entry.
	mcpBytes, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var mcp map[string]any
	json.Unmarshal(mcpBytes, &mcp)
	servers := mcp["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Errorf("expected 1 server entry, got %d", len(servers))
	}
}

func TestSetupMergesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Pre-populate settings.json with existing keys.
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	existing := map[string]any{
		"env":          map[string]any{"MY_VAR": "hello"},
		"teammateMode": "tmux",
		"permissions":  map[string]any{"allow": []any{"Read"}},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), data, 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	settingsBytes, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var settings map[string]any
	json.Unmarshal(settingsBytes, &settings)

	// Existing keys preserved.
	env, ok := settings["env"].(map[string]any)
	if !ok || env["MY_VAR"] != "hello" {
		t.Error("existing env key not preserved")
	}
	if settings["teammateMode"] != "tmux" {
		t.Error("existing teammateMode not preserved")
	}
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Error("existing permissions not preserved")
	} else if _, ok := perms["allow"]; !ok {
		t.Error("existing permissions.allow not preserved")
	}

	// Hook was added.
	hooks := settings["hooks"].(map[string]any)
	entries := hooks["UserPromptSubmit"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 hook, got %d", len(entries))
	}
}

func TestSetupMergesExistingMCPJSON(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Pre-populate .mcp.json with another server.
	existing := map[string]any{
		"mcpServers": map[string]any{
			"other-server": map[string]any{
				"command": "other-bin",
				"args":    []any{"run"},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, ".mcp.json"), data, 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	mcpBytes, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var mcp map[string]any
	json.Unmarshal(mcpBytes, &mcp)
	servers := mcp["mcpServers"].(map[string]any)

	// Other server preserved.
	if _, ok := servers["other-server"]; !ok {
		t.Error("existing other-server not preserved in .mcp.json")
	}

	// Mycelium added.
	mycelium, ok := servers["mycelium"].(map[string]any)
	if !ok {
		t.Fatal("mycelium server not added")
	}
	if mycelium["command"] != "mctl" {
		t.Errorf("expected command 'mctl', got %q", mycelium["command"])
	}
}

func TestSetupMctlPathFlag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup", "--mctl-path", "/usr/local/bin/mctl"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Check .mcp.json uses custom path.
	mcpBytes, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var mcp map[string]any
	json.Unmarshal(mcpBytes, &mcp)
	servers := mcp["mcpServers"].(map[string]any)
	mycelium := servers["mycelium"].(map[string]any)
	if mycelium["command"] != "/usr/local/bin/mctl" {
		t.Errorf("expected custom mctl-path in .mcp.json, got %q", mycelium["command"])
	}

	// Check skill file uses custom path.
	skill, _ := os.ReadFile(filepath.Join(dir, ".claude", "skills", "mycelium", "SKILL.md"))
	if !strings.Contains(string(skill), "Bash(/usr/local/bin/mctl *)") {
		t.Error("skill file does not contain custom mctl-path in allowed-tools")
	}
	if !strings.Contains(string(skill), "/usr/local/bin/mctl init") {
		t.Error("skill file does not use custom mctl-path in commands table")
	}
}

func TestSetupOverwritesStaleSkill(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Write a stale skill file.
	skillDir := filepath.Join(dir, ".claude", "skills", "mycelium")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("old content"), 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	skill, _ := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if string(skill) == "old content" {
		t.Error("stale skill file was not overwritten")
	}
	if !strings.Contains(string(skill), "name: mycelium") {
		t.Error("skill file missing expected content after overwrite")
	}
}

func TestSetupPreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Pre-populate settings.json with an existing UserPromptSubmit hook.
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	existing := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{
				map[string]any{
					"matcher": "",
					"command": "echo 'existing hook'",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), data, 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	settingsBytes, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var settings map[string]any
	json.Unmarshal(settingsBytes, &settings)
	hooks := settings["hooks"].(map[string]any)
	entries := hooks["UserPromptSubmit"].([]any)

	if len(entries) != 2 {
		t.Fatalf("expected 2 hook entries (existing + mycelium), got %d", len(entries))
	}

	// Verify the existing hook is still there.
	first := entries[0].(map[string]any)
	if first["command"] != "echo 'existing hook'" {
		t.Error("existing hook was not preserved")
	}

	// Verify mycelium hook was appended.
	second := entries[1].(map[string]any)
	if second["command"] != hookCommand {
		t.Error("mycelium hook was not appended")
	}
}

func TestSetupErrorsOnMalformedSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte("{invalid json"), 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on malformed settings.json")
	}
	if !strings.Contains(err.Error(), "parse existing") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestSetupErrorsOnMalformedMCPJSON(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{invalid json"), 0644)

	cmd := rootCmd
	cmd.SetArgs([]string{"setup"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on malformed .mcp.json")
	}
	if !strings.Contains(err.Error(), "parse existing") {
		t.Errorf("expected parse error, got: %v", err)
	}
}
