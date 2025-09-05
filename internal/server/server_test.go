package server_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

// Test-specific values
var (
	ctx    context.Context
	logger *slog.Logger
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	ctx = context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	exitCode := m.Run()
	os.Exit(exitCode)

}
