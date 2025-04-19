/*
This *should* be a separate package (main_test) to avoid direct access to
private methods, but I want to test gracefulShutdown without exporting it.
*/
package main

import (
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"golang.org/x/net/html"
)

/* Connection details for the test database */
const (
	dbName = "main_test"
	dbUser = "main_user"
	dbPass = "main_pass"
)

var (
	dbHostPort  string
	logger      *slog.Logger
	obsHostPort string
)

// Sets up the application tests.
// Creates a Postrgres database container to use for testing.
// Sets up a logger for the tests
func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewJSONHandler(os.Stderr, options)
	logger = slog.New(handler)

	/* Set up a Docker container pool and connect to it */
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal("could not set up docker pool ", err)
	}

	err = pool.Client.Ping()
	if err != nil {
		log.Fatal("could not connect to docker ", err)
	}

	dbResource, err := pool.RunWithOptions(&dockertest.RunOptions{
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

	dbHostPort = dbResource.GetHostPort("5432/tcp")
	dbUrl := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", dbUser, dbPass, dbHostPort, dbName)

	/* Tell docker to hard kill the container in 120 seconds */
	dbResource.Expire(30)

	/*
		Exponential backoff-retry, because the application in the container might not
		be ready to accept connections yet
	*/
	pool.MaxWait = 10 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", dbUrl)
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	/* Clean up container resources when the testing is done */
	defer func() {
		if err := pool.Purge(dbResource); err != nil {
			log.Fatalf("Could not purge database resource: %s", err)
		}
	}()

	/*
		Create a test container with the observability endpoints (checked as part of
		the health check)
	*/
	obsResource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository:   "grafana/otel-lgtm",
		Tag:          "0.11.0",
		ExposedPorts: []string{"3000/tcp"},
		Env: []string{
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

	obsHostPort = obsResource.GetHostPort("3000/tcp")
	obsUrl := fmt.Sprintf("http://%s/api/health", obsHostPort)

	/* Tell docker to hard kill the container in 120 seconds */
	obsResource.Expire(30)

	/*
		Exponential backoff-retry, because the application in the container might not
		be ready to accept connections yet
	*/
	pool.MaxWait = 30 * time.Second
	if err = pool.Retry(func() error {
		_, err := http.Get(obsUrl)
		if err != nil && err != io.EOF {
			return err
		}
		return nil
	}); err != nil {
		log.Fatalf("Could not connect to observability docker: %s", err)
	}

	/* Clean up container obsResources when the testing is done */
	defer func() {
		if err := pool.Purge(obsResource); err != nil {
			log.Fatalf("Could not purge obsResource: %s", err)
		}
	}()

	m.Run()

}

// Tests the health check endpoint
// Connects to the testing database container
// Starts an application server
// Calls the health check endpoint and validates the output
func TestHealthCheck(t *testing.T) {

	ctx := context.Background()

	testData := []struct {
		dbError                   bool
		dbPort                    string
		expectedDBStatusClass     string
		expectedDBHealthEntries   int
		expectedHttpStatus        int
		expectedObservStatusClass string
		healthy                   string
		observError               bool
		statusMismatchErrMsg      string
		testName                  string
	}{
		{dbError: false, dbPort: dbHostPort, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Successful health check"},
		{dbError: false, dbPort: dbHostPort, expectedDBHealthEntries: 0, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusInternalServerError, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 500 response", testName: "Invalid templates dir"},
		{dbError: true, dbPort: dbHostPort, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Database error"},
		{dbError: false, dbPort: dbHostPort, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Observability error"},
		{dbError: true, dbPort: dbHostPort, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Nothing healthy"},
	}

	for _, data := range testData {

		port := freePort()

		env := map[string]string{
			"DB_USER": dbUser,
			"DB_PASS": dbPass,
			"DB_HOST": strings.Split(dbHostPort, ":")[0],
			"DB_PORT": strings.Split(dbHostPort, ":")[1],
			"DB_NAME": dbName,
			"OTEL_HC": fmt.Sprintf("http://%s/api/health", obsHostPort),
			"PORT":    strconv.Itoa(port),
		}

		if data.testName == "Invalid templates dir" {

			env["TEMPLATES_DIR"] = "templates"

		} else {

			env["TEMPLATES_DIR"] = "../../cmd/web/templates"

		}

		getenv := func(key string) string { return env[key] }

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()
			db, err := database.Connection(getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			appHandler, err := server.NewServer(getenv, db, logger)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/health", nil)
			if err != nil {
				t.Fatal("error building health check request", err)
			}

			/* Fake a database error by just closing the databse (if applicable) */
			if data.dbError {

				db.Close()

			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal("server call failed", err)
			}
			defer res.Body.Close()

			if res.StatusCode != data.expectedHttpStatus {

				t.Fatal(data.statusMismatchErrMsg)

			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing the HTML content from the response", err)
			}

			/*
				Don't try to validate document contents if there was an HTTP error
			*/
			if data.expectedDBHealthEntries != http.StatusOK {

				return

			}

			/*
				I don't care about the actual values per se (and if I make this parallel
				they won't be reliable), I just care that I'm picking up the correct number
				of data points and that they have data.
			*/
			dbStatusFound, observStatusFound := false, false
			var dbHealthItems int = 0
			for node := range doc.Descendants() {

				if slices.Contains(node.Attr, html.Attribute{Key: "id", Val: "db-health-status"}) {

					dbStatusFound = true

					if !slices.Contains(node.Attr, html.Attribute{Key: "class", Val: data.expectedDBStatusClass}) {

						t.Fatal("invalid database health status class, expected ", data.expectedDBStatusClass)

					}

				}

				if slices.Contains(node.Attr, html.Attribute{Key: "id", Val: "observ-health-status"}) {

					observStatusFound = true

					if !slices.Contains(node.Attr, html.Attribute{Key: "class", Val: data.expectedDBStatusClass}) {

						t.Fatal("invalid database health status class, expected ", data.expectedDBStatusClass)

					}

				}

				if node.Type == html.ElementNode &&
					slices.Contains(node.Attr, html.Attribute{Key: "id", Val: "overall-health"}) &&
					node.FirstChild.Data != "" {

					if node.FirstChild.Data != data.healthy {

						t.Fatalf("Expected an overall health status of %s, but was %s", data.healthy, node.FirstChild.Data)

					}

				}

				if node.Type == html.ElementNode &&
					slices.Contains(node.Attr, html.Attribute{Key: "class", Val: "db-health-item"}) &&
					node.FirstChild.Data != "" {

					dbHealthItems++

				}

			}

			if !dbStatusFound {

				t.Fatal("no database health status found!")

			}

			if !observStatusFound {

				t.Fatal("no observability health status found!")

			}

			if data.expectedDBHealthEntries != dbHealthItems {

				t.Fatal("Expected", data.expectedDBHealthEntries,
					" database health items in the application health report, but got",
					dbHealthItems, "instead")

			}

		})
	}
}

// Tests the shutdown handler
// Starts an application server
// Triggers a shutdown signal to shut the server down
func TestShutdown(t *testing.T) {

	ctx := context.Background()

	testData := []struct {
		testName string
	}{
		{testName: "Graceful shutdown"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			done := make(chan bool, 1)
			server := &http.Server{}

			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			gracefulShutdown(ctx, server, done, func(context.Context) error { return nil }, logger)

			completed := <-done

			if !completed {

				t.Fatal("Expected the shutdown to have completed gracefully!")

			}

		})

	}
}

/*
Asks the system for an open port I can use for a server or container
Pulled from https://stackoverflow.com/a/43425461
*/
func freePort() (port int) {

	if listener, err := net.Listen("tcp", ":0"); err == nil {

		port = listener.Addr().(*net.TCPAddr).Port

	} else {

		log.Fatal("error getting open port", err)

	}

	return

}
