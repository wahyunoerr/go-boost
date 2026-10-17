package main

import (
	"os"
	"testing"
)

func TestCLICommandsExecuteWithoutActivation(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	commands := [][]string{
		{"go-boost", "version"},
		{"go-boost", "help"},
		{"go-boost", "status"},
		{"go-boost", "routes"},
	}

	for _, args := range commands {
		os.Args = args
		if code := run(); code != exitOK {
			t.Errorf("expected command %v to succeed (exitOK=%d), got %d", args, exitOK, code)
		}
	}
}

func TestUnknownCommandReturnsExitError(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"go-boost", "nonexistent-command"}
	if code := run(); code != exitError {
		t.Fatalf("expected unknown command to return exitError (%d), got %d", exitError, code)
	}
}
