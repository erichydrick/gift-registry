package middleware_test

import (
	"context"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
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

	dbPath := filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")
	dbCont, dbURL, err := test.BuildDBContainer(ctx, dbPath, dbName, dbUser, dbPass)
	defer func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}()
	if err != nil {
		log.Fatal("Error setting up a test database", err)
	}

	env := map[string]string{
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
	os.Exit(exitCode)

}
