package database_test

import (
	"database/sql"
	"fmt"
	"gift-registry/internal/database"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

/* Connection details for the test database */
const (
	dbName = "database_test"
	dbUser = "database_user"
	dbPass = "database_pass"
)

/* hostAndPort gets reset for different tests */
var (
	hostAndPort string
)

// Sets up the database package tests.
// Creates a Postgres container for testing, configuring it to automatically clean itself up.
// Starts the testing database container.
func TestMain(m *testing.M) {

	log.Println("Creating the embedded Postgres for database testing")

	/* Set up a Docker container pool and connect to it */
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal("could not set up docker pool ", err)
	}

	err = pool.Client.Ping()
	if err != nil {
		log.Fatal("could not connect to docker ", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "17.2",
		Env: []string{
			fmt.Sprintf("POSTGRES_PASSWORD=%s", dbPass),
			fmt.Sprintf("POSTGRES_USER=%s", dbUser),
			fmt.Sprintf("POSTGRES_DB=%s", dbName),
			"listen_addresses = '*'",
		},
	}, func(config *docker.HostConfig) {
		/* Autoremove = true ensures stopped containers are removed */
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatal("could not start docker ", err)
	}

	hostAndPort = resource.GetHostPort("5432/tcp")
	databaseUrl := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", dbUser, dbPass, hostAndPort, dbName)

	log.Println("Connecting to database on url: ", databaseUrl)

	/* Tell docker to hard kill the container in 120 seconds */
	resource.Expire(10)

	/*
		Exponential backoff-retry, because the application in the container might not
		be ready to accept connections yet
	*/
	pool.MaxWait = 10 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", databaseUrl)
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	/* Clean up container resources when the testing is done */
	defer func() {
		if err := pool.Purge(resource); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
	}()

	m.Run()
}

// Tests connecting to the database and confirms the function behaves correctly
// when successful and when connection fails due to a bad config.
func TestConnect(t *testing.T) {

	testData := []struct {
		hostAndPort   string
		errorExpected bool
		testName      string
	}{
		{hostAndPort: hostAndPort, errorExpected: false, testName: "Successful connection"},
		{hostAndPort: hostAndPort + "0", errorExpected: true, testName: "Failed connection"},
	}

	for _, data := range testData {

		env := map[string]string{
			"DB_USER": dbUser,
			"DB_PASS": dbPass,
			"DB_HOST": strings.Split(data.hostAndPort, ":")[0],
			"DB_PORT": strings.Split(data.hostAndPort, ":")[1],
			"DB_NAME": dbName,
		}

		getenv := func(key string) string { return env[key] }

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			db, err := database.Connection(getenv)
			if !data.errorExpected && err != nil {

				t.Fatal("successful connection attempt failed! ", err)

			} else if data.errorExpected && err == nil {

				t.Fatal("have a connection even though it should have failed!")

			}

			if db != nil {

				/* I'll test this separately later */
				defer db.Close()

			}

		})

	}

}

// Tests the Close() function from the database package
// This test doesn't do much, as the function is a wrapper around
// sql.DB.Close(), but this at least leaves us infrastructure in place
// should that ever change
func TestClose(t *testing.T) {

	// TODO: FIGURE OUT THE TEST DATA HERE
	testData := []struct {
		testName string
	}{
		{testName: "Successful close"},
	}

	for _, data := range testData {

		env := map[string]string{
			"DB_USER": dbUser,
			"DB_PASS": dbPass,
			"DB_HOST": strings.Split(hostAndPort, ":")[0],
			"DB_PORT": strings.Split(hostAndPort, ":")[1],
			"DB_NAME": dbName,
		}

		getenv := func(key string) string { return env[key] }

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			db, err := database.Connection(getenv)
			if err != nil {
				t.Fatal("Error connecting to the database for a real Close() call. ", err)
			}

			err = database.Close()
			if err != nil {
				t.Fatal("Error testing a successful Close() ", err)
			}
			/*
				Simple error - back-to-back closes. The second should fail because we're closing a closed DB
			*/

			/* This SHOULD fail, we just closed the connection */
			err = db.Ping()
			if err == nil {
				t.Fatal("Just pinged a closed database connection!")
			}

		})

	}

}
