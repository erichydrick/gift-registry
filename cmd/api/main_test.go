/*
This *should* be a separate package (main_test) to avoid direct access to
private methods, but I want to test gracefulShutdown without exporting it.
*/
package main

import (
	"context"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	grafanalgtm "github.com/testcontainers/testcontainers-go/modules/grafana-lgtm"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"golang.org/x/net/html"
)

type containers struct {
	dbCont  *postgres.PostgresContainer
	dbUrl   string
	obsCont *grafanalgtm.GrafanaLGTMContainer
	obsUrl  string
}

/* Connection details for the test database */
const (
	dbName = "main_test"
	dbUser = "main_user"
	dbPass = "main_pass"
)

var (
	ctx    context.Context
	dbUrl  string
	logger *slog.Logger
	obsUrl string
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	m.Run()

}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheck(t *testing.T) {

	testData := []struct {
		dbError                   bool
		expectedDBStatusClass     string
		expectedDBHealthEntries   int
		expectedHttpStatus        int
		expectedObservStatusClass string
		healthy                   string
		observError               bool
		statusMismatchErrMsg      string
		testName                  string
	}{
		{dbError: false, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Successful health check"},
		{dbError: false, expectedDBHealthEntries: 0, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusInternalServerError, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 500 response", testName: "Invalid templates dir"},
		{dbError: true, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Database error"},
		{dbError: false, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Observability error"},
		{dbError: true, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, statusMismatchErrMsg: "Expected an HTTP 200 response", testName: "Nothing healthy"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			testContainers, err := buildTestContainers(ctx)
			defer func() {
				if err := testcontainers.TerminateContainer(testContainers.obsCont); err != nil {
					log.Fatal("Failed to terminate the observability test container ", err)
				}
			}()
			defer func() {
				if err := testcontainers.TerminateContainer(testContainers.dbCont); err != nil {
					log.Fatal("Failed to terminate the database test container ", err)
				}
			}()
			if err != nil {
				t.Fatal("Error setting up test containers! ", err)
			}

			port := freePort()

			env := map[string]string{
				"DB_USER":        dbUser,
				"DB_PASS":        dbPass,
				"DB_HOST":        strings.Split(dbUrl, ":")[0],
				"DB_PORT":        strings.Split(dbUrl, ":")[1],
				"DB_NAME":        dbName,
				"OTEL_HC":        fmt.Sprintf("%s/api/health", obsUrl),
				"PORT":           strconv.Itoa(port),
				"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations_test/success"),
			}

			if data.testName == "Invalid templates dir" {

				env["TEMPLATES_DIR"] = "templates"

			} else {

				env["TEMPLATES_DIR"] = "../../cmd/web/templates"

			}

			getenv := func(key string) string { return env[key] }

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			appHandler, err := server.NewServer(getenv, db.DB, logger)
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

// TestShutdown validates the shutdown handler by starting an application server
// and triggering a shutdown signal to shut the server down
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

func buildTestContainers(ctx context.Context) (containers, error) {

	obsCont, err := grafanalgtm.Run(
		ctx,
		"grafana/otel-lgtm:0.11.0",
	)
	if err != nil {
		return containers{}, fmt.Errorf("failed to launch the observability test container! %v", err)
	}

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
		return containers{}, fmt.Errorf("failed to launch the database test container! %v", err)
	}

	dbUrl, err = dbCont.Endpoint(ctx, "")
	if err != nil {
		return containers{}, fmt.Errorf("error getting the database endpoint %v", err)
	}

	obsUrl, err = obsCont.Endpoint(ctx, "http")
	if err != nil {
		return containers{}, fmt.Errorf("error getting the observability endpoint %v", err)
	}

	return containers{dbCont: dbCont, dbUrl: dbUrl, obsCont: obsCont, obsUrl: obsUrl}, nil

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
