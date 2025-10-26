package health_test

import (
	"context"
	"database/sql"
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
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/net/html"
)

type testDB struct {
	db *sql.DB
}

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

var (
	ctx    context.Context
	db     database.Database
	dbURL  string
	env    map[string]string
	getenv func(string) string
	logger *slog.Logger
	port   int
	start  time.Time
)

func TestMain(m *testing.M) {

	start = time.Now().Local()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	var dbCont *postgres.PostgresContainer
	var err error

	dbCont, dbURL, err = test.BuildDBContainer(ctx, filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql"), dbName, dbUser, dbPass)
	defer func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}()
	if err != nil {
		log.Fatal("Error setting up test containers! ", err)
	}

	port = test.FreePort()

	env = map[string]string{
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_HOST":        strings.Split(dbURL, ":")[0],
		"DB_PORT":        strings.Split(dbURL, ":")[1],
		"DB_NAME":        dbName,
		"PORT":           strconv.Itoa(port),
		"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations"),
		"TEMPLATES_DIR":  filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(key string) string { return env[key] }

	db, err = database.Connection(ctx, logger, func(key string) string { return env[key] })
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	exitCode := m.Run()
	os.Exit(exitCode)

}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheck(t *testing.T) {

	testData := []struct {
		dbError               bool
		expectedDBStatusClass string
		expectedHttpStatus    int
		healthy               string
		testName              string
	}{
		{
			dbError:               false,
			expectedDBStatusClass: "healthy",
			expectedHttpStatus:    http.StatusOK,
			healthy:               "Healthy",
			testName:              "Successful health check",
		},
		{
			dbError:               true,
			expectedDBStatusClass: "unhealthy",
			expectedHttpStatus:    http.StatusOK,
			healthy:               "Unhealthy",
			testName:              "Database error",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			/*
				When we need to simulate a database error, we'll close the connection.
				Because I want to run these tests in parallel, I can't close the same
				connection the healthy database tests use, so create a duplicate that
				I'll close instead. For healthy database tests, use the existing
				connection reference.
			*/
			var testDB database.Database
			var err error
			if data.dbError {

				testDB, err = throwawayDB()
				if err != nil {
					t.Fatal("Error setting up a throwaway database connection for testing a database failure!", err)
				}

			} else {

				testDB = db

			}

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, testDB, logger, emailer)
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

				testDB.Close()

			}

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
			if data.expectedHttpStatus != http.StatusOK {

				return

			}

			/*
				I don't care about the actual values per se (and if I make this parallel
				they won't be reliable), I just care that I'm picking up the correct number
				of data points and that they have data.
			*/
			dbStatusFound := false
			for node := range doc.Descendants() {

				if slices.Contains(node.Attr, html.Attribute{Key: "id", Val: "db-health-status"}) {

					dbStatusFound = true

					if !slices.Contains(node.Attr, html.Attribute{Key: "class", Val: data.expectedDBStatusClass}) {

						t.Fatal("invalid database health status class, expected ", data.expectedDBStatusClass)

					}

				}

				if node.Type == html.ElementNode &&
					slices.Contains(node.Attr, html.Attribute{Key: "id", Val: "overall-health"}) &&
					node.FirstChild.Data != "" {

					if strings.TrimSpace(node.FirstChild.Data) != data.healthy {

						t.Fatalf("Expected an overall health status of %s, but was %s", data.healthy, node.FirstChild.Data)

					}

				}

			}

			if !dbStatusFound {

				t.Fatal("no database health status found!")

			}

		})

	}

}

func throwawayDB() (database.Database, error) {

	url := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable&timezone=UTC",
		dbUser,
		dbPass,
		strings.Split(dbURL, ":")[0],
		strings.Split(dbURL, ":")[1],
		dbName,
	)

	conn, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("error opening a throwaway database connection: %v", err)
	}

	if err = conn.Ping(); err != nil {
		return nil, fmt.Errorf("error pinging the throwaway database connection: %v", err)
	}

	return testDB{db: conn}, nil

}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheckInvalidTemplate(t *testing.T) {

	env = map[string]string{
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_HOST":        strings.Split(dbURL, ":")[0],
		"DB_PORT":        strings.Split(dbURL, ":")[1],
		"DB_NAME":        dbName,
		"PORT":           strconv.Itoa(port),
		"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations"),
		"TEMPLATES_DIR":  "templates",
	}
	getenv = func(key string) string { return env[key] }

	testData := []struct {
		dbError               bool
		expectedDBStatusClass string
		expectedHttpStatus    int
		healthy               string
		templates             string
		testName              string
	}{
		{
			dbError:               false,
			expectedDBStatusClass: "healthy",
			expectedHttpStatus:    http.StatusInternalServerError,
			healthy:               "Healthy",
			templates:             "templates",
			testName:              "Invalid templates dir",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

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

		})

	}

}

func (db testDB) Close() error {

	return db.db.Close()

}

func (db testDB) Execute(ctx context.Context, statement string, params ...any) (sql.Result, error) {

	return nil, sql.ErrNoRows

}

func (db testDB) Ping(ctx context.Context) error {

	return db.db.Ping()

}

func (db testDB) Query(ctx context.Context, query string, params ...any) (*sql.Rows, error) {

	return nil, sql.ErrNoRows

}

func (db testDB) QueryRow(ctx context.Context, query string, params ...any) *sql.Row {

	return nil

}
