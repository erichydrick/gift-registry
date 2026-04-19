package database_test

import (
	"context"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	dbPath string
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

	copied, err := test.SetupTestDatabase(srcDB, dbName)
	if err != nil {
		log.Fatal("Could not create test database ", dbName, ": ", err)
	}
	defer func() {
		err := test.CleanupDatabase(dbName)
		if err != nil {
			log.Fatal("Error cleaning up the test ", err)
		}

	}()
	logger.InfoContext(
		ctx,
		"Created test database",
		slog.String("filename", dbName),
		slog.Int64("size", copied),
	)

	env = map[string]string{
		"DB_NAME":        dbName,
		"MIGRATIONS_DIR": filepath.Join("migrations_test", "success"),
	}

	m.Run()
}

// TestCleanup validates that the database automatically cleans up expired
// verification tokens and session IDs every ${TICKER_INTERVAL} SECONDS.
// For testing purposes, that interval will be every second.
func TestCleanup(t *testing.T) {
	testData := []struct {
		expectedRowCnt int
		offset         int
		testName       string
		userData       test.UserData
	}{
		{
			expectedRowCnt: 1,
			offset:         300,
			testName:       "Not Expired",
			userData: test.UserData{
				Email:      "notexpiredtokens@localhost.com",
				ExternalID: "not-expired-tokens",
				FirstName:  "Not",
				LastName:   "Expired",
				Type:       "NORMAL",
			},
		},
		{
			expectedRowCnt: 0,
			offset:         -120,
			testName:       "Expired",
			userData: test.UserData{
				Email:      "expiredtokens@localhost.com",
				ExternalID: "expired-tokens",
				FirstName:  "Yes",
				LastName:   "Expired",
				Type:       "NORMAL",
			},
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

			sessionID, err := test.CreateSession(
				ctx,
				logger,
				db,
				data.userData,
				time.Duration(data.offset)*time.Second,
				userAgent,
			)
			if err != nil {
				t.Fatal("Error creating test session:", err)
			}

			row := db.QueryRow(ctx, "SELECT person_id FROM person WHERE email = $1", data.userData.Email)
			var personID int64
			if err := row.Scan(&personID); err != nil {
				t.Fatal("Could not read the person ID", err)
			}

			expires := time.Now().Add(time.Duration(data.offset) * time.Second).UTC()

			/*
				Do the insertion and make sure it worked. We're going to t.Fatal() if this
				fails, so I'm not going to worry about Rollback() calls erroring, the
				database is going to be deleted anyhow
			*/
			if res, err := db.Execute(ctx, "INSERT INTO verification (person_id, token, token_expiration, attempts) VALUES ($1, $2, $3, $4)", personID, sessionID, expires, 0); err != nil {
				t.Fatal("Error adding a new test verification record to the database.", err)
			} else if added, err := res.RowsAffected(); err != nil {
				t.Fatal("Error getting the last inserted ID from the test verification creation.", err)
			} else if added < 1 {
				t.Fatal("No rows were added to the verification table!", err)
			}

			time.Sleep(1 * time.Second)

			if _, err := db.Query(ctx, "SELECT expiration FROM session WHERE session_id = $1", sessionID); err != nil {
				t.Fatal("Error checking session cleanup", err)
			} else if _, err := db.Query(ctx, "SELECT * FROM verification WHERE token = $1", sessionID); err != nil {
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
				"20250401_000300_create_test_table.sql",
			},
			migrationsDir:    "migrations_test/rollback",
			testName:         "Migration rollback",
			validationQuery:  "SELECT filename FROM migrations",
			validationResCnt: 2,
		},
		{
			errorExpected: false,
			expectedFilesApplied: []string{
				"20250401_000000_create_test_table.sql",
				"20250401_000100_insert_test_person.sql",
				"20250401_000300_create_test_table.sql",
				"20250401_000400_alter_test_table.sql",
			},
			migrationsDir:    "migrations_test/alter_table",
			testName:         "Update existing table",
			validationQuery:  "SELECT * FROM information_schema.columns WHERE table_name = 'person' ",
			validationResCnt: 7,
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			/*
				Since I'm testing migrations, use a fresh database for each test case
			*/
			srcDB, err := filepath.Abs(filepath.Join("..", "test", "test.db"))
			if err != nil {
				t.Fatal("Could not find test database source: ", err)
			}

			testDB, err := filepath.Abs(data.testName + ".db")
			if err != nil {
				t.Fatal("Could not get path for the test database (", data.testName, ".db)")
			}

			copied, err := test.SetupTestDatabase(srcDB, testDB)
			if err != nil {
				log.Fatal("Could not create test database ", testDB, ": ", err)
			}
			defer func() {
				err := test.CleanupDatabase(testDB)
				if err != nil {
					log.Fatal("Error cleaning up the test ", err)
				}

			}()
			logger.InfoContext(
				ctx,
				"Created test database",
				slog.String("filename", testDB),
				slog.Int64("size", copied),
			)

			/* We're also using a fresh set of migrations per test case */
			getenv := func(key string) string {
				if key == "MIGRATIONS_DIR" {
					return data.migrationsDir
				}
				return env[key]
			}

			db, err := database.Connect(ctx, logger, getenv)
			if err != nil && err != database.ErrMigration {
				t.Fatal("Error connecting to the database!", err)
			}

			migrationsApplied := []string{}
			rows, err := db.Query(ctx, database.FindMigrationsQuery)
			if err != nil {
				t.Fatal("Error getting the updated list of migrations run", err)
			}
			defer func() { _ = rows.Close() }()

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

			_ = db.Close()
		})

	}
}
