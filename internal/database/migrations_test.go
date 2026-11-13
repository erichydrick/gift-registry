package database_test

import (
	"gift-registry/internal/database"
	"gift-registry/internal/test"
	"log"
	"path/filepath"
	"testing"
)

// TestRunMigrations validates the migrations runner and confirms the
// migrations files are applied correctly and the transaction properly
// rolls back in case of a problem
/*
	TODO:
	ALL SCHEMA MIGRATIONS ARE PICKED UP AS EMBEDDED FS, SO THIS TESTS DATA
	MIGRATIONS EXCLUSIVELY. RE-WRITE THE TESTS AS DATA MIGRATIONS (INCLUDING NO
	DATA MIGRATIONS DIR AND NOT PRESENT DIR), AND THE DB START WILL VALIDATE THE
	SCHEMA MIGRATIONS FOR FREE.
*/
func TestRunMigrations(t *testing.T) {
	testData := []struct {
		errorExpected        bool
		expectedFilesApplied map[string]bool
		migrationsDir        string
		testName             string
		validationQuery      string
		validationResCnt     int
	}{
		{
			errorExpected: false,
			expectedFilesApplied: map[string]bool{
				"insert_session_data.sql":               false,
				"insert_test_person_success.sql":        false,
				"insert_second_test_person_success.sql": false,
			},
			migrationsDir:    filepath.Join("..", "..", "testing_data", "database", "sql"),
			testName:         "Successful migration",
			validationQuery:  "SELECT * FROM person WHERE id IN (555, 777, 888, 999)",
			validationResCnt: 2,
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
				if key == "DATA_MIGRATIONS_DIR" {
					return data.migrationsDir
				}
				return env[key]
			}

			db, err := database.Connect(ctx, logger, getenv)
			if err != nil && err != database.ErrMigration {
				t.Fatal("Error connecting to the database!", err)
			}

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

				if _, ok := data.expectedFilesApplied[filename]; ok {

					data.expectedFilesApplied[filename] = true

				}

			}

			allMigrationsApplied := true
			/* Validate we ran the files we expected */
			for _, applied := range data.expectedFilesApplied {

				allMigrationsApplied = allMigrationsApplied && applied

				/* Stop if we ever find that we didn't apply a file */
				if !applied {
					break
				}

			}

			if !allMigrationsApplied {

				t.Fatal("Missing at least 1 expected applied migration", data.expectedFilesApplied)

			}

			_ = db.Close()
		})

	}
}
