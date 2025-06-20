package health_test

import (
	"context"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"log/slog"
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
	"golang.org/x/net/html"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

var (
	ctx    context.Context
	logger *slog.Logger
)

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheck(t *testing.T) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	/*
		Spin up a Grafana container to use for the observability part of the health
		check. Doing it here because we don't need a separate copy per test case.
	*/
	obsCont, err := grafanalgtm.Run(
		ctx,
		"grafana/otel-lgtm:0.11.7",
	)
	defer func() {
		if err := testcontainers.TerminateContainer(obsCont); err != nil {
			log.Fatal("Failed to terminate the observability test container ", err)
		}
	}()
	if err != nil {
		log.Fatalf("Failed to launch the observability test container! %v", err)
	}

	obsURL, err := obsCont.Endpoint(ctx, "http")
	if err != nil {
		log.Fatalf("Error getting the observability endpoint %v", err)
	}

	testData := []struct {
		dbError                   bool
		expectedDBStatusClass     string
		expectedDBHealthEntries   int
		expectedHttpStatus        int
		expectedObservStatusClass string
		healthy                   string
		observError               bool
		testName                  string
	}{
		{dbError: false, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, testName: "Successful health check"},
		{dbError: false, expectedDBHealthEntries: 0, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusInternalServerError, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, testName: "Invalid templates dir"},
		{dbError: true, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "healthy", healthy: "Healthy", observError: false, testName: "Database error"},
		{dbError: false, expectedDBHealthEntries: 6, expectedDBStatusClass: "healthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, testName: "Observability error"},
		{dbError: true, expectedDBHealthEntries: 0, expectedDBStatusClass: "unhealthy", expectedHttpStatus: http.StatusOK, expectedObservStatusClass: "unhealthy", healthy: "Healthy", observError: true, testName: "Nothing healthy"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			dbCont, dbURL, err := test.BuildDBContainer(ctx, filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql"), dbName, dbUser, dbPass)
			defer func() {
				if err := testcontainers.TerminateContainer(dbCont); err != nil {
					log.Fatal("Failed to terminate the database test container ", err)
				}
			}()
			if err != nil {
				t.Fatal("Error setting up test containers! ", err)
			}

			port := test.FreePort()

			env := map[string]string{
				"DB_USER":        dbUser,
				"DB_PASS":        dbPass,
				"DB_HOST":        strings.Split(dbURL, ":")[0],
				"DB_PORT":        strings.Split(dbURL, ":")[1],
				"DB_NAME":        dbName,
				"OTEL_HC":        fmt.Sprintf("%s/api/health", obsURL),
				"PORT":           strconv.Itoa(port),
				"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations"),
			}

			if data.testName == "Invalid templates dir" {

				env["TEMPLATES_DIR"] = "templates"

			} else {

				env["TEMPLATES_DIR"] = filepath.Join("..", "..", "cmd", "web", "templates")

			}

			getenv := func(key string) string { return env[key] }

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			err = test.CreateUser(ctx, db)
			if err != nil {
				t.Fatal("Error setting up a test user!", err)
			}

			token, err := test.CreateSession(ctx, db, 5*time.Minute, test.DefaultUserAgent)
			if err != nil {
				t.Fatal("Error setting up test user session!", err)
			}

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, db, logger, emailer)
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

			/* Set up session info */
			sessCookie := http.Cookie{}
			sessCookie.Name = server.SessionCookie
			sessCookie.MaxAge = time.Now().UTC().Add(5 * time.Minute).Second()
			sessCookie.HttpOnly = true
			sessCookie.Secure = true
			sessCookie.SameSite = http.SameSiteStrictMode
			sessCookie.Value = token
			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)

			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				logger.Error(fmt.Sprintf("Server call failed %v", err))
			}

			if res.StatusCode != data.expectedHttpStatus {

				t.Fatal("Expected a ", data.expectedHttpStatus, "status, but got a ", res.StatusCode, "response")

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
