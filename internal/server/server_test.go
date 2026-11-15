package server_test

import (
	"context"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/templates"
	"gift-registry/internal/test"
)

// Connection details for the test database
const (
	dbName = "server_test_database"
)

// Test-specific values
var (
	ctx              context.Context
	db               database.Database
	elementsFilePath string
	emailer          server.Emailer
	getenv           func(string) string
	logger           *slog.Logger
	testServer       *httptest.Server
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {
	ctx = context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	templates.Initialize(logger)
	dbPath, err := filepath.Abs(filepath.Join(dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	env := map[string]string{
		"DB_NAME":         dbPath,
		"DATA_MIGRATIONS": filepath.Join("..", "..", "testing_data", "server_data", "sql"),
		"MIGRATIONS_DIR":  filepath.Join("..", "database", "migrations"),
		"TEMPLATES_DIR":   filepath.Join("..", "..", "cmd", "web", "templates"),
	}

	getenv = func(name string) string { return env[name] }

	db, err = database.Connect(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	emailer = &test.EmailMock{
		EmailToToken: map[string]string{},
		EmailToSent:  map[string]bool{},
	}

	elementsFilePath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "server_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not build path to expected element data for validating output!", err)
	}

	appHandler, err := server.NewServer(getenv, db, logger, emailer)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testServer = httptest.NewServer(appHandler)
	defer testServer.Close()

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)
}
