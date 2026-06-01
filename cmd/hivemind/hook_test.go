package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestHookPreToolUse_Allow(t *testing.T) {
	// Backup os.Stdin and os.Stdout
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	// 1. Prepare Stdin Pipe
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}
	os.Stdin = rIn

	// Set CLI flag args for subcommand "hook PreToolUse"
	// We override os.Args to simulate "hivemind hook PreToolUse"
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"hivemind", "hook", "PreToolUse"}

	payload := AntigravityInput{
		ConversationID: "test-conv-123",
		ToolCall: &AntigravityToolCall{
			Name: "view_file",
			Args: map[string]interface{}{"AbsolutePath": "/test.txt"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	go func() {
		_, _ = wIn.Write(payloadBytes)
		_ = wIn.Close()
	}()

	// 2. Prepare Stdout Pipe
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	os.Stdout = wOut

	// 3. Run hook logic with mock socket
	runHook("/tmp/test_hivemind_telemetry.sock", "PreToolUse")

	_ = wOut.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := buf.String()

	// Assert correct response
	var response map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &response); err != nil {
		t.Fatalf("Response is not valid JSON: %s, err: %v", output, err)
	}

	if response["decision"] != "allow" {
		t.Errorf("Expected decision to be 'allow', got %v", response["decision"])
	}
}

func TestHookPostToolUse(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	rIn, wIn, _ := os.Pipe()
	os.Stdin = rIn

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"hivemind", "hook", "PostToolUse"}

	payload := AntigravityInput{
		ConversationID: "test-conv-123",
		StepIdx:        5,
		Error:          "command failed",
	}
	payloadBytes, _ := json.Marshal(payload)

	go func() {
		_, _ = wIn.Write(payloadBytes)
		_ = wIn.Close()
	}()

	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	runHook("/tmp/test_hivemind_telemetry.sock", "PostToolUse")

	_ = wOut.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := buf.String()

	var response map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &response); err != nil {
		t.Fatalf("Response is not valid JSON: %s", output)
	}

	if len(response) != 0 {
		t.Errorf("Expected empty response object for PostToolUse, got: %v", response)
	}
}

func TestHookPreInvocation(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	rIn, wIn, _ := os.Pipe()
	os.Stdin = rIn

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"hivemind", "hook", "PreInvocation"}

	payload := AntigravityInput{
		ConversationID:  "test-conv-123",
		InvocationNum:   2,
		InitialNumSteps: 12,
	}
	payloadBytes, _ := json.Marshal(payload)

	go func() {
		_, _ = wIn.Write(payloadBytes)
		_ = wIn.Close()
	}()

	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	runHook("/tmp/test_hivemind_telemetry.sock", "PreInvocation")

	_ = wOut.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := buf.String()

	var response map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &response); err != nil {
		t.Fatalf("Response is not valid JSON: %s", output)
	}

	injectSteps, exists := response["injectSteps"]
	if !exists {
		t.Errorf("Expected 'injectSteps' in response, got %v", response)
	}
	if list, ok := injectSteps.([]interface{}); !ok || len(list) != 0 {
		t.Errorf("Expected empty list for 'injectSteps', got %v", injectSteps)
	}
}

func TestHookStop(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	rIn, wIn, _ := os.Pipe()
	os.Stdin = rIn

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"hivemind", "hook", "Stop"}

	payload := AntigravityInput{
		ConversationID:    "test-conv-123",
		FullyIdle:         true,
		TerminationReason: "model_stop",
	}
	payloadBytes, _ := json.Marshal(payload)

	go func() {
		_, _ = wIn.Write(payloadBytes)
		_ = wIn.Close()
	}()

	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	runHook("/tmp/test_hivemind_telemetry.sock", "Stop")

	_ = wOut.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := buf.String()

	var response map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &response); err != nil {
		t.Fatalf("Response is not valid JSON: %s", output)
	}

	if response["decision"] != "allow" {
		t.Errorf("Expected decision to be 'allow', got %v", response["decision"])
	}
}
