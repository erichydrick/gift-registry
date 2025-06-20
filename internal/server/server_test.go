package server_test

import (
	"context"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

// Test-specific values
var (
	browsers []playwright.BrowserType
	ctx      context.Context
	logger   *slog.Logger
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	ctx = context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	/* Install playwright dependencies */
	err := playwright.Install()
	if err != nil {
		log.Fatal("Error installing Playwright dependencies! ", err)
	}

	browsers, err = test.BrowserList()
	if err != nil {
		log.Fatal("Error building browser list!", err)
	}

	exitCode := m.Run()
	os.Exit(exitCode)

}
