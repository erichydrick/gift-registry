package middleware_test

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
	"gift-registry/internal/test"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

var (
	allowedMethods []string
	ctx            context.Context
	db             database.Database
	getenv         func(string) string
	logger         *slog.Logger
	testServer     *httptest.Server
)

// TestMain will set up the server for testing the various middleware
// functionalities. It will spin up 1 application instace for the test suite
// to keep the tests fast, as well as define common variables that will be
// re-used throughout tests
func TestMain(m *testing.M) {

	ctx = context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	srcDB, err := filepath.Abs(filepath.Join("..", "test", "test.db"))
	if err != nil {
		log.Fatal("Could not find test database source: ", err)
	}

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	copied, err := test.SetupTestDatabase(srcDB, dbPath)
	if err != nil {
		log.Fatal("Could not create test database ", dbPath, ": ", err)
	}
	logger.InfoContext(
		ctx,
		"Created test database",
		slog.String("filename", dbPath),
		slog.Int64("size", copied),
	)

	env := map[string]string{
		"DB_NAME":          dbName,
		"MIGRATIONS_DIR":   filepath.Join("..", "..", "internal", "database", "migrations"),
		"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
		"TEMPLATES_DIR":    filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(name string) string { return env[name] }

	db, err = database.Connect(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	emailer := &test.EmailMock{
		EmailToToken: map[string]string{},
		EmailToSent:  map[string]bool{},
	}
	appHandler, err := server.NewServer(getenv, db, logger, emailer)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testServer = httptest.NewServer(appHandler)
	defer testServer.Close()

	allowedMethods = []string{"OPTIONS", "GET", "POST"}

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)
}
