package health_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"

	"golang.org/x/net/html"
)

type unhealthyDatabase struct {
	db database.Database
}

// Connection details for the test database
const (
	dbName    = "health_check_handlers_database"
	userAgent = "test-user-agent"
)

var (
	badDB            database.Database
	ctx              context.Context
	elementsFilePath string
	env              map[string]string
	getenv           func(string) string
	liveDB           database.Database
	logger           *slog.Logger
	port             int
	start            time.Time
)

func TestMain(m *testing.M) {

	start = time.Now().Local()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	srcDB, err := filepath.Abs(filepath.Join("..", "test", "test.db"))
	if err != nil {
		log.Fatal("Could not find test database source: ", err)
	}

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	copied, err := test.SetupTestDatabase(srcDB, dbPath)
	if err != nil {
		log.Fatal("Could not create test database ", dbPath, ": ", err)
	}

	logger.InfoContext(
		ctx,
		"Created test database",
		slog.String("filename", dbPath),
		slog.Int64("size", copied),
	)

	env = map[string]string{
		"DB_NAME":        dbPath,
		"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations"),
		"TEMPLATES_DIR":  filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(key string) string { return env[key] }

	liveDB, err = database.Connect(ctx, logger, func(key string) string { return env[key] })
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	badDB = unhealthyDatabase{
		db: liveDB,
	}

	elementsFilePath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "health_check_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not load files for output validation!")
	}

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)

}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheck(t *testing.T) {
	testData := []struct {
		db                 database.Database
		elementsFile       string
		expectedHttpStatus int
		testName           string
	}{
		{
			db:                 liveDB,
			elementsFile:       "health_check_healthy_response.json",
			expectedHttpStatus: http.StatusOK,
			testName:           "Successful health check",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, data.db, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/health", nil)
			if err != nil {
				t.Fatal("error building health check request", err)
			}
			logger.DebugContext(
				ctx,
				"Ready to send a health check request",
				slog.String("uri", req.URL.Port()),
				slog.String("expectedStatus", data.elementsFile),
			)

			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("User-Agent", userAgent)

			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
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

			elementsData, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Error loading the field validation data", err)
			}

			logger.DebugContext(
				ctx,
				"Validating the response",
				slog.String("port", res.Request.URL.Port()),
				slog.String("filename", data.elementsFile),
				slog.Any("elements", elementsData),
			)
			err = test.ValidatePage(doc, elementsData)
			if err != nil {
				t.Fatal("Error validating the health check output", err)
			}

		})
	}
}

// TestHealthCheckBadDB validates the health check endpoint by connecting to the
// testing database container, starting an application server with an
// "unhealthy" database, calling the health check endpoint, and validating the
// output
func TestHealthCheckBadDB(t *testing.T) {
	testData := []struct {
		db                 database.Database
		elementsFile       string
		expectedHttpStatus int
		testName           string
	}{
		{
			db:                 badDB,
			elementsFile:       "health_check_unhealthy_db_response.json",
			expectedHttpStatus: http.StatusOK,
			testName:           "Database error",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, data.db, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/health", nil)
			if err != nil {
				t.Fatal("error building health check request", err)
			}
			logger.DebugContext(
				ctx,
				"Ready to send a health check request",
				slog.String("uri", req.URL.Port()),
				slog.String("expectedStatus", data.elementsFile),
			)

			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("User-Agent", userAgent)

			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
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

			elementsData, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Error loading the field validation data", err)
			}

			logger.DebugContext(
				ctx,
				"Validating the response",
				slog.String("port", res.Request.URL.Port()),
				slog.String("filename", data.elementsFile),
				slog.Any("elements", elementsData),
			)
			err = test.ValidatePage(doc, elementsData)
			if err != nil {
				t.Fatal("Error validating the health check output", err)
			}

		})
	}
}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheckInvalidTemplate(t *testing.T) {
	env = map[string]string{
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
			appHandler, err := server.NewServer(getenv, liveDB, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/health", nil)
			if err != nil {
				t.Fatal("error building health check request", err)
			}

			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
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

/*
Implement the database.Database interface so I can simulate a bad database
connection during a health check
*/
func (badDB unhealthyDatabase) Close() error {
	return badDB.db.Close()
}

func (badDB unhealthyDatabase) Execute(ctx context.Context, statement string, params ...any) (sql.Result, error) {
	return badDB.db.Execute(ctx, statement, params...)
}

func (badDB unhealthyDatabase) ExecuteBatch(ctx context.Context, statements []string, params [][]any) ([]sql.Result, []error) {
	return badDB.db.ExecuteBatch(ctx, statements, params)
}

func (badDB unhealthyDatabase) Ping(_ context.Context) error {
	return fmt.Errorf("assume the database is down now")
}

func (badDB unhealthyDatabase) Query(ctx context.Context, query string, params ...any) (*sql.Rows, error) {
	return badDB.db.Query(ctx, query, params...)
}

func (badDB unhealthyDatabase) QueryRow(ctx context.Context, query string, params ...any) *sql.Row {
	return badDB.db.QueryRow(ctx, query, params...)
}
