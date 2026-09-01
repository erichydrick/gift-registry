package database_test

import (
	"gift-registry/internal/database"
	"gift-registry/internal/test"
	"log"
	"path/filepath"
	"slices"
	"testing"
)

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

			testDB, err := filepath.Abs(data.testName + ".db")
			if err != nil {
				t.Fatal("Could not get path for the test database (", data.testName, ".db)")
			}

			defer func() {
				err := test.CleanupDatabase(testDB)
				if err != nil {
					log.Fatal("Error cleaning up the test ", err)
				}

			}()

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
