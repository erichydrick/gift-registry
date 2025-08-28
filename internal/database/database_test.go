package database_test

import (
	"context"
	"gift-registry/internal/database"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

/* Connection details for the test database */
const (
	dbName = "main_test"
	dbUser = "database_user"
	dbPass = "database_pass"
)

var (
	ctx    context.Context
	dbPath string
	env    map[string]string
	logger *slog.Logger
)

func init() {
	dbPath = filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")
}

// TestMain sets up the database package tests initializing the logger used
// to set up the database connection.
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

	env = map[string]string{
		"DB_HOST":        strings.Split(dbURL, ":")[0],
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_PORT":        strings.Split(dbURL, ":")[1],
		"DB_NAME":        dbName,
		"MIGRATIONS_DIR": filepath.Join("migrations_test", "success"),
	}

	m.Run()
}

// TestConnect validates connecting to the database and confirms the
// Connect() function behaves correctly when successful and when
// connection fails due to a bad config.
func TestConnect(t *testing.T) {

	testData := []struct {
		errorExpected bool
		migrationsDir string
		portModifier  string
		testName      string
	}{
		{
			errorExpected: false,
			testName:      "Successful connection",
		},
		{
			errorExpected: true,
			portModifier:  "0",
			testName:      "Failed connection",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			getenv := func(name string) string {
				if name == "DB_PORT" {
					return env[name] + data.portModifier
				}
				return env[name]
			}

			db, err := database.Connection(ctx, logger, getenv)
			if !data.errorExpected && err != nil {

				t.Fatal(t.Name(), ": successful connection attempt failed! ", err)

			} else if data.errorExpected && err == nil {

				db.Close()
				t.Fatal(t.Name(), ": have a connection even though it should have failed!")

			}

			db.Close()

		})

	}

}

// TestRunMigrations validates the migrations runner and confirms the
// migrations files are applied correctly and the transaction properly
// rolls back in case of a problem
func TestRunMigrations(t *testing.T) {

	testData := []struct {
		errorExpected        bool
		expectedFilesApplied []string
		migrationsDir        string
		testName             string
		validationQuery      string
		validationResCnt     int
	}{
		{
			errorExpected: false,
			expectedFilesApplied: []string{
				"20250401_000000_create_test_table.sql",
				"20250401_000100_insert_test_person.sql",
			},
			migrationsDir:    "migrations_test/success",
			testName:         "Successful migration",
			validationQuery:  "SELECT * FROM person WHERE email = 'test.user@yopmail.com'",
			validationResCnt: 1,
		},
		{
			errorExpected: true,
			expectedFilesApplied: []string{
				"20250401_000000_create_test_table.sql",
				"20250401_000100_insert_test_person.sql",
			},
			migrationsDir:    "migrations_test/rollback",
			testName:         "Migration rollback",
			validationQuery:  "SELECT filename FROM migrations",
			validationResCnt: 1,
		},
		{
			errorExpected: false,
			expectedFilesApplied: []string{
				"20250401_000000_create_test_table.sql",
				"20250401_000100_insert_test_person.sql",
				"20250401_000300_alter_test_table.sql",
			},
			migrationsDir:    "migrations_test/alter_table",
			testName:         "Update existing table",
			validationQuery:  "SELECT * FROM information_schema.columns WHERE table_name = 'person' ",
			validationResCnt: 6,
		},
	}

	for _, data := range testData {

		getenv := func(key string) string {
			if key == "MIGRATIONS_DIR" {
				return data.migrationsDir
			}
			return env[key]
		}

		t.Run(data.testName, func(t *testing.T) {

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil && err != database.ErrMigration {
				t.Fatal("Error connecting to the database!", err)
			}

			migrationsApplied := []string{}
			rows, err := db.Query(ctx, database.FindMigrationsQuery)
			if err != nil {
				t.Fatal("Error getting the updated list of migrations run", err)
			}
			defer rows.Close()

			for rows.Next() {
				var filename string
				if err := rows.Scan(&filename); err != nil {
					t.Fatal("Error mapping result to filename")
				}
				migrationsApplied = append(migrationsApplied, filename)

			}

			/* Validate we ran the files we expected */
			if !slices.Equal(data.expectedFilesApplied, migrationsApplied) {
				t.Fatal("Expected list of applied migrations to be ", data.expectedFilesApplied, " but was ", migrationsApplied)
			}

			db.Close()

		})

	}

}
