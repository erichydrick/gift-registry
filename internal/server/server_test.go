package server_test

import (
	"context"
	"database/sql"
	"gift-registry/internal/database"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
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
	dbCont   *postgres.PostgresContainer
	dbURL    string
	db       *sql.DB
	env      map[string]string
	getenv   func(string) string
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

	dbCont, dbURL, err = test.BuildDBContainer(ctx, dbPath, dbName, dbUser, dbPass)
	defer func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}()
	if err != nil {
		log.Fatal("Error setting up a test database", err)
	}

	env = map[string]string{
		"DB_HOST":          strings.Split(dbURL, ":")[0],
		"DB_USER":          dbUser,
		"DB_PASS":          dbPass,
		"DB_PORT":          strings.Split(dbURL, ":")[1],
		"DB_NAME":          dbName,
		"MIGRATIONS_DIR":   filepath.Join("..", "..", "internal", "database", "migrations"),
		"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
		"TEMPLATES_DIR":    filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(name string) string { return env[name] }

	db, err = database.Connection(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	exitCode := m.Run()
	os.Exit(exitCode)

}
