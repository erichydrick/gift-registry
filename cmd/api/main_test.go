package main_test

import (
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
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
	hostAndPort string
	logger      *slog.Logger
)

// Sets up the application tests.
// Creates a Postrgres database container to use for testing.
// Sets up a logger for the tests
func TestMain(m *testing.M) {

	log.Println("Creating the embedded Postgres for application testing")

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

// Tests the health check endpoint
// Connects to the testing database container
// Starts an application server
// Calls the health check endpoint and validates the output
func TestHealthCheck(t *testing.T) {

	ctx := context.Background()

	testData := []struct {
		dbHostAndPort         string
		expectedHealthEntries int
		expectedHttpStatus    int
		statusMismatchErrMsg  string
		templatesDir          string
		testName              string
	}{
		{dbHostAndPort: hostAndPort, expectedHealthEntries: 6, expectedHttpStatus: http.StatusOK, statusMismatchErrMsg: "Expected an HTTP 200 response", templatesDir: "../../cmd/web/templates", testName: "Successful health check"},
		{dbHostAndPort: hostAndPort, expectedHealthEntries: 0, expectedHttpStatus: http.StatusInternalServerError, statusMismatchErrMsg: "Expected an HTTP 200 response", templatesDir: "templates", testName: "Invalid templates dir"},
	}

	for _, data := range testData {

		port := freePort()

		env := map[string]string{
			"DB_USER":       dbUser,
			"DB_PASS":       dbPass,
			"DB_HOST":       strings.Split(data.dbHostAndPort, ":")[0],
			"DB_PORT":       strings.Split(data.dbHostAndPort, ":")[1],
			"DB_NAME":       dbName,
			"PORT":          strconv.Itoa(port),
			"TEMPLATES_DIR": data.templatesDir,
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
				I don't care about the actual values per se (and if I make this parallel
				they won't be reliable), I just care that I'm picking up the correct number
				of data points and that they have data.
			*/
			var dbHealthItems int = 0
			for node := range doc.Descendants() {

				if node.Type == html.ElementNode &&
					slices.Contains(node.Attr, html.Attribute{Key: "class", Val: "db-health-item"}) &&
					node.FirstChild.Data != "" {

					dbHealthItems++
				}

			}

			if data.expectedHealthEntries != dbHealthItems {

				t.Fatal("Expected", data.expectedHealthEntries,
					" database health items in the application health report, but got",
					dbHealthItems, "instead")

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
