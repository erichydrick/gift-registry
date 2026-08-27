package database_test

import (
	"context"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gift-registry/internal/database"
	"gift-registry/internal/test"
)

/* Connection details for the test database */
const (
	dbName    = "database_test.db"
	userAgent = "test-user-agent"
)

var (
	ctx    context.Context
	env    map[string]string
	logger *slog.Logger
)

// TestMain sets up the database package tests initializing the logger used
// to set up the database connection.
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

	env = map[string]string{
		"DATA_MIGRATIONS": filepath.Join("..", "..", "testing_data", "database_data", "sql"),
		"DB_NAME":         dbPath,
		"MIGRATIONS_DIR":  filepath.Join("migrations_test", "success"),
	}

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)

}

// TestCleanup validates that the database automatically cleans up expired
// verification tokens and session IDs every ${TICKER_INTERVAL} SECONDS.
// For testing purposes, that interval will be every second.
func TestCleanup(t *testing.T) {
	testData := []struct {
		expectedRowCnt int
		offset         int
		testName       string
		token          string
		userData       test.UserData
	}{
		{
			expectedRowCnt: 1,
			offset:         300,
			testName:       "Not Expired",
			token:          "not-expired-tokens",
		},
		{
			expectedRowCnt: 0,
			offset:         -120,
			testName:       "Expired",
			token:          "expired-tokens",
		},
	}
	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			getenv := func(name string) string {
				if name == "TICKER_INTERVAL" {
					return "100"
				}
				return env[name]
			}

			db, err := database.Connect(ctx, logger, getenv)
			if err != nil {
				t.Fatal("Error setting up the database connection:", err)
			}

			time.Sleep(1 * time.Second)

			if _, err := db.Query(ctx, "SELECT expiration FROM session WHERE session_id = $1", data.token); err != nil {

				t.Fatal("Error checking session cleanup", err)

			} else if _, err := db.Query(ctx, "SELECT * FROM verification WHERE token = $1", data.token); err != nil {

				t.Fatal("Verification token did not clean up!", err)

			}

		})

	}

}

// TestConnect validates connecting to the database and confirms the
// Connect() function behaves correctly when successful and when
// connection fails due to a bad config.
func TestConnect(t *testing.T) {
	testData := []struct {
		dbName        string
		errorExpected bool
		migrationsDir string
		testName      string
	}{
		{
			dbName:        dbName,
			errorExpected: false,
			testName:      "Successful connection",
		},
		{
			dbName:        dbName + ".not_on_fs",
			errorExpected: true,
			testName:      "Failed connection",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			getenv := func(name string) string {
				if name == "DB_NAME" {
					return data.dbName
				}
				return env[name]
			}

			db, err := database.Connect(ctx, logger, getenv)
			if !data.errorExpected && err != nil {
				t.Fatal(t.Name(), ": successful connection attempt failed! ", err)
			} else if data.errorExpected && err == nil {

				_ = db.Close()
				t.Fatal(t.Name(), ": have a connection even though it should have failed!")

			}

			_ = db.Close()
		})
	}
}
