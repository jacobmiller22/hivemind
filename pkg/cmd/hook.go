package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Hook(ctx context.Context, args []string) error {
	// Root hook command is a routing placeholder
	return nil
}

type HookConfig struct {
	Command string `json:"command"`
}

type HookMatcher struct {
	Matcher string       `json:"matcher"`
	Hooks   []HookConfig `json:"hooks"`
}

type DebugHooks struct {
	PreToolUse     []HookMatcher `json:"PreToolUse"`
	PostToolUse    []HookMatcher `json:"PostToolUse"`
	PreInvocation  []HookConfig  `json:"PreInvocation"`
	PostInvocation []HookConfig  `json:"PostInvocation"`
	Stop           []HookConfig  `json:"Stop"`
}

type HooksJSON struct {
	DebugHooks DebugHooks `json:"debug-hooks"`
}

func HookAntigravity20(ctx context.Context, args []string) error {
	fmt.Println("[*] Starting Antigravity 2.0 telemetry hook installation...")

	// 1. Determine absolute path of current executable
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine absolute path of hivemind executable: %w", err)
	}
	fmt.Printf("  Resolved executable path: %s\n", selfPath)

	// 2. Resolve active workspace directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get active workspace directory: %w", err)
	}
	fmt.Printf("  Active workspace path: %s\n", cwd)

	// 3. Create .agents folder if it does not exist
	agentsDir := filepath.Join(cwd, ".agents")
	fmt.Printf("[*] Checking customization folder: %s\n", agentsDir)
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		fmt.Println("  Customization folder does not exist, creating directory...")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			return fmt.Errorf("failed to create agents directory %s: %w", agentsDir, err)
		}
	}
	fmt.Println("✔ Customization folder is ready")

	// 4. Generate hooks.json content
	hooksData := HooksJSON{
		DebugHooks: DebugHooks{
			PreToolUse: []HookMatcher{
				{
					Matcher: "*",
					Hooks: []HookConfig{
						{Command: fmt.Sprintf("%s event antigravity PreToolUse", selfPath)},
					},
				},
			},
			PostToolUse: []HookMatcher{
				{
					Matcher: "*",
					Hooks: []HookConfig{
						{Command: fmt.Sprintf("%s event antigravity PostToolUse", selfPath)},
					},
				},
			},
			PreInvocation: []HookConfig{
				{Command: fmt.Sprintf("%s event antigravity PreInvocation", selfPath)},
			},
			PostInvocation: []HookConfig{
				{Command: fmt.Sprintf("%s event antigravity PostInvocation", selfPath)},
			},
			Stop: []HookConfig{
				{Command: fmt.Sprintf("%s event antigravity Stop", selfPath)},
			},
		},
	}

	hooksFile := filepath.Join(agentsDir, "hooks.json")
	fmt.Printf("[*] Writing workspace hook configuration to: %s\n", hooksFile)

	fd, err := os.OpenFile(hooksFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open hooks.json for writing: %w", err)
	}
	defer fd.Close()

	encoder := json.NewEncoder(fd)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(hooksData); err != nil {
		return fmt.Errorf("failed to write hook configuration data: %w", err)
	}

	fmt.Println("✔ Copied hooks.json successfully")
	fmt.Println("\n=========================================")
	fmt.Println("✔ Workspace Hook Installed Successfully!")
	fmt.Println("=========================================")
	fmt.Println("👉 Verification Checklist:")
	fmt.Println("  1. Verify the hooks configuration matches requirements by tailing '.agents/hooks.json'.")
	fmt.Println("  2. Run an Antigravity agent in this workspace to automatically push turn-by-turn telemetry.")
	fmt.Println("=========================================")

	return nil
}
