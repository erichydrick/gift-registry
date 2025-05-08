package database_test

import (
	"context"
	"errors"
	"gift-registry/internal/database"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

/* Connection details for the test database */
const (
	dbName = "main_test"
	dbUser = "database_user"
	dbPass = "database_pass"
)

var (
	ctx    context.Context
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

	m.Run()
}

// TestConnect validates connecting to the database and confirms the
// Connect() function behaves correctly when successful and when
// connection fails due to a bad config.
func TestConnect(t *testing.T) {

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("Error getting the current working directory ", err.Error())
	}

	testData := []struct {
		errorExpected bool
		migrationsDir string
		portModifier  string
		testName      string
	}{
		{errorExpected: false, migrationsDir: "migrations", testName: "Successful connection"},
		{errorExpected: true, migrationsDir: "migrations", portModifier: "0", testName: "Failed connection"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			hostAndPort, cleanup := buildTestContainer(ctx, t)
			defer cleanup()

			env := map[string]string{
				"DB_USER":        dbUser,
				"DB_PASS":        dbPass,
				"DB_HOST":        strings.Split(hostAndPort, ":")[0],
				"DB_PORT":        strings.Split(hostAndPort, ":")[1] + data.portModifier,
				"DB_NAME":        dbName,
				"MIGRATIONS_DIR": filepath.Join(cwd, data.migrationsDir),
			}

			getenv := func(key string) string { return env[key] }

			db, err := database.Connection(ctx, logger, getenv)
			if !data.errorExpected && err != nil {

				t.Fatal("successful connection attempt failed! ", err)

			} else if data.errorExpected && err == nil {

				t.Fatal(t.Name(), ": have a connection even though it should have failed!")

			}

			if db.DB != nil {

				/* I'll test this separately later */
				db.Close()

			}

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
		{errorExpected: false, expectedFilesApplied: []string{"00_create_tables.sql", "01_insert_person.sql"}, migrationsDir: "migrations_test/success", testName: "Successful migration", validationQuery: "SELECT * FROM person WHERE email = 'test.user@yopmail.com'", validationResCnt: 1},
		{errorExpected: true, expectedFilesApplied: []string{"00_create_tables.sql"}, migrationsDir: "migrations_test/rollback", testName: "Migration rollback", validationQuery: "SELECT filename FROM migrations", validationResCnt: 1},
		{errorExpected: false, expectedFilesApplied: []string{"00_create_tables.sql", "01_modify_columns.sql"}, migrationsDir: "migrations_test/alter_table", testName: "Update existing table", validationQuery: "SELECT * FROM information_schema.columns WHERE table_name = 'person' ", validationResCnt: 9},
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("Error getting the current working directory ", err.Error())
	}

	for _, data := range testData {

		env := map[string]string{
			"DB_USER":        dbUser,
			"DB_PASS":        dbPass,
			"MIGRATIONS_DIR": filepath.Join(cwd, data.migrationsDir),
		}

		getenv := func(key string) string { return env[key] }

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			hostAndPort, cleanup := buildTestContainer(ctx, t)
			defer cleanup()

			env["DB_HOST"] = strings.Split(hostAndPort, ":")[0]
			env["DB_PORT"] = strings.Split(hostAndPort, ":")[1]
			env["DB_NAME"] = dbName

			/* Just want a do-nothing context placeholder */
			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				/*
					It's only a failure if we expected the migration to succeed (or didn't get
					a migration error)
				*/
				if !data.errorExpected || !errors.Is(err, database.ErrMigration) {
					t.Fatal("Error setting up test database connection! ", err)
				}
			}
			defer db.Close()

			/*
				Confirm the error value I got back is what I expected.
			*/
			if data.errorExpected != (err != nil) {
				t.Fatal("Error migrations error expected? ", data.errorExpected, " Error returned? ", (err != nil))
			}

			migrationsApplied := []string{}
			rows, err := db.DB.QueryContext(ctx, "SELECT filename FROM migrations")
			if err != nil {
				t.Fatal("Error getting the updated list of migrations run")
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

			rows, err = db.DB.QueryContext(ctx, data.validationQuery)
			if err != nil {
				t.Fatal("Could not run independent validation query")
			}
			defer rows.Close()

			rowsReturned := 0
			for rows.Next() {

				rowsReturned++

			}

			if rowsReturned != data.validationResCnt {
				t.Fatalf("Expected %d results to be returned, but got %d", data.validationResCnt, rowsReturned)
			}

		})

	}

}

func buildTestContainer(ctx context.Context, t *testing.T) (string, func()) {

	dbCont, err := postgres.Run(
		ctx,
		"postgres:17.2",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPass),
		postgres.WithInitScripts(filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		log.Fatal("Failed to launch the database test container! ", err)
	}

	dbURL, err := dbCont.Endpoint(ctx, "")
	if err != nil {
		t.Fatal("Did not get a connection endpoint to the Postgres container ", err)
	}

	_, err = dbCont.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal("Error getting the connection string! ", err)
	}

	/*
		Return a clean-up function so we can remove the container resources when
		the testing is done
	*/
	return dbURL, func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}

}
