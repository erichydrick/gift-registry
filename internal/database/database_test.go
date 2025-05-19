package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/database"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

/* Connection details for the test database */
const (
	dbUser = "database_user"
	dbPass = "database_pass"
)

/* hostAndPort gets reset for different tests */
var (
	logger *slog.Logger
	pool   *dockertest.Pool
)

// Sets up the database package tests.
// Creates a Postgres container for testing, configuring it to automatically clean itself up.
// Starts the testing database container.
func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewJSONHandler(os.Stderr, options)
	logger = slog.New(handler)

	log.Println("Creating the embedded Postgres for database testing")

	/* Set up a Docker container pool and connect to it */
	var err error
	pool, err = dockertest.NewPool("")
	if err != nil {
		log.Fatal("could not set up docker pool ", err)
	}

	err = pool.Client.Ping()
	if err != nil {
		log.Fatal("could not connect to docker ", err)
	}

	m.Run()
}

// Tests connecting to the database and confirms the function behaves correctly
// when successful and when connection fails due to a bad config.
func TestConnect(t *testing.T) {

	testData := []struct {
		errorExpected bool
		portModifier  string
		testName      string
	}{
		{errorExpected: false, testName: "Successful connection"},
		{errorExpected: true, portModifier: "0", testName: "Failed connection"},
	}

	for _, data := range testData {

		env := map[string]string{
			"DB_USER": dbUser,
			"DB_PASS": dbPass,
		}

		getenv := func(key string) string { return env[key] }

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			hostAndPort, dbName, cleanup := buildTestContainer(t.Name())
			defer cleanup()

			env["DB_HOST"] = strings.Split(hostAndPort, ":")[0]
			env["DB_PORT"] = strings.Split(hostAndPort, ":")[1] + data.portModifier
			env["DB_NAME"] = dbName

			log.Println("Environment variables for ", t.Name(), ", ", env)
			log.Println("Container for test ", t.Name(), ": ", hostAndPort)
			db, err := database.Connection(logger, getenv)
			if !data.errorExpected && err != nil {

				t.Fatal("successful connection attempt failed! ", err)

			} else if data.errorExpected && err == nil {

				log.Println(t.Name(), ": Error expected = ", data.errorExpected, " and err = ", err)
				t.Fatal(t.Name(), ": have a connection even though it should have failed!")

			}

			if db != nil {

				/* I'll test this separately later */
				database.Close()

			}

		})

	}

}

// Tests the migrations runner and confirms the migrations files are applied
// correctly and the transaction properly rolls back in case of a problem
func TestRunMigrations(t *testing.T) {

	testData := []struct {
		errorExpected       bool
		expectedRowCnts     map[string]int64
		migrationsDir       string
		supplementalDir     string
		supplementalRowCnts map[string]int64
		testName            string
	}{
		{errorExpected: false, expectedRowCnts: map[string]int64{"00_create_tables.sql": 1}, migrationsDir: "migrations_test/success", testName: "Successful migration"},
		{errorExpected: true, expectedRowCnts: map[string]int64{}, migrationsDir: "migrations_test/rollback", testName: "Migration rollback"},
		{errorExpected: false, expectedRowCnts: map[string]int64{"00_create_tables.sql": 1}, migrationsDir: "migrations_test/success", supplementalDir: "migrations_test/second", supplementalRowCnts: map[string]int64{"01_follow_up_migration": 1}, testName: "Update existing migration"},
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

			hostAndPort, dbName, cleanup := buildTestContainer(t.Name())
			defer cleanup()

			env["DB_HOST"] = strings.Split(hostAndPort, ":")[0]
			env["DB_PORT"] = strings.Split(hostAndPort, ":")[1]
			env["DB_NAME"] = dbName

			/* Just want a do-nothing context placeholder */
			ctx := context.Background()
			/* TODO: GET THIS REFERENCE SO WE CAN CHECK DB STATE LATER */
			_, err := database.Connection(logger, getenv)
			if err != nil {
				t.Fatal("Error setting up test database connection! ", err)
			}
			defer database.Close()

			fileToRowCnts, err := database.RunMigrations(ctx, logger, getenv)

			/*
				Confirm the error value I got back is what I expected.
			*/
			if data.errorExpected != (err != nil) {
				t.Fatal("Error migrations error expected? ", data.errorExpected, " Error returned? ", (err != nil))
			}

			if !reflect.DeepEqual(data.expectedRowCnts, fileToRowCnts) {

				t.Fatal("File to row count modified mappings didn't match the expected value. Expected ", fmt.Sprintf("%v", data.expectedRowCnts), " but got ", fmt.Sprintf("%v", fileToRowCnts))

			}

			/* TODO: ADD QUERY TO VERIFY SUCCESSFUL MIGRATION FILES WERE ADDED TO THE DATABASE */

			if data.supplementalDir != "" {

				env["MIGRATIONS_DIR"] = data.supplementalDir
				fileToRowCnts, err := database.RunMigrations(ctx, logger, getenv)
				if err != nil {
					t.Fatal("Unexpected error doing a follow-up migration: ", err.Error())
				}

				if !reflect.DeepEqual(data.supplementalRowCnts, fileToRowCnts) {

					t.Fatal("File to row count modified mappings for the supplemental migration didn't match the expected value. Expected ", fmt.Sprintf("%v", data.supplementalRowCnts)+" but got "+fmt.Sprintf("%v", fileToRowCnts))

				}

			}

			/* TODO: ADD QUERY TO VERIFY SUCCESSFUL MIGRATION FILES WERE ADDED TO THE DATABASE */

		},
		)
	}

}

func buildTestContainer(testName string) (string, string, func()) {

	testName = strings.ReplaceAll(testName, "/", "__")
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("Error getting current working directory")
	}
	initScript := filepath.Join(cwd, "..", "..", "docker", "postgres_scripts", "init.sql")

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "17.2",
		Env: []string{
			fmt.Sprintf("POSTGRES_PASSWORD=%s", dbPass),
			fmt.Sprintf("POSTGRES_USER=%s", dbUser),
			fmt.Sprintf("POSTGRES_DB=%s", testName),
			"listen_addresses = '*'",
		},
		Mounts: []string{initScript + ":/docker-entrypoint-initdb.d/init.sql"},
	}, func(config *docker.HostConfig) {
		/* Autoremove = true ensures stopped containers are removed */
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatal("could not start docker ", err)
	}

	hostAndPort := resource.GetHostPort("5432/tcp")
	databaseUrl := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", dbUser, dbPass, hostAndPort, testName)

	/* Tell docker to hard kill the container in 10 seconds */
	resource.Expire(10)

	/*
		Exponential backoff-retry, because the application in the container might not
		be ready to accept connections yet
	*/
	pool.MaxWait = 10 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", databaseUrl)
		defer db.Close()
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	/*
		Return a clean-up function so we can remove the container resources when
		the testing is done
	*/
	return hostAndPort, testName, func() {
		if err := pool.Purge(resource); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
	}

}
