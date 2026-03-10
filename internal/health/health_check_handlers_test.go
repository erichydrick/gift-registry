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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gift-registry/internal/database"
	"gift-registry/internal/middleware"
	"gift-registry/internal/server"
	"gift-registry/internal/test"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/net/html"
)

type unhealthyDatabase struct {
	db database.Database
}

// Connection details for the test database
const (
	dbName    = "server_test"
	dbUser    = "server_user"
	dbPass    = "server_pass"
	userAgent = "test-user-agent"
)

var (
	badDB  database.Database
	ctx    context.Context
	liveDB database.Database
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
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
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

	liveDB, err = database.Connect(ctx, logger, func(key string) string { return env[key] })
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	badDB = unhealthyDatabase{
		db: liveDB,
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}

// TestHealthCheck validates the health check endpoint by connecting to the
// testing database container, starting an application server, calling the
// health check endpoint, and validating the output
func TestHealthCheck(t *testing.T) {
	testData := []struct {
		db                    database.Database
		expectedDBStatusClass string
		expectedHttpStatus    int
		healthy               string
		testName              string
		userData              test.UserData
	}{
		{
			db:                    liveDB,
			expectedDBStatusClass: "healthy",
			expectedHttpStatus:    http.StatusOK,
			healthy:               "Healthy",
			testName:              "Successful health check",
			userData: test.UserData{
				Email:         "successfulHealthCheck@localhost.com",
				ExternalID:    "success-health-check",
				FirstName:     "Success",
				HouseholdName: "Health Check",
				LastName:      "Check",
			},
		},
		{
			db:                    badDB,
			expectedDBStatusClass: "unhealthy",
			expectedHttpStatus:    http.StatusOK,
			healthy:               "Unhealthy",
			testName:              "Database error",
			userData: test.UserData{
				Email:         "badDBHealthCheck@localhost.com",
				ExternalID:    "bad-db-health-check",
				FirstName:     "Bad",
				HouseholdName: "Health Check",
				LastName:      "Database",
			},
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			/*
				Not using test.db here because when I hit the test case of a "bad"
				database connectino it fails before it starts (because that "databse"
				isn't actually real). So always use the live DB for this part, then use
				the test.db input for everything else.
			*/
			token, err := test.CreateSession(
				ctx,
				logger,
				liveDB,
				data.userData,
				time.Second*5,
				userAgent,
			)
			if err != nil {
				t.Fatal("Could not create a test session to validate health checking", err)
			}

			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Second * 5).Second(),
				Name:     middleware.SessionCookie,
				Secure:   true,
				Value:    token,
			}

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
			req.AddCookie(&sessCookie)
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
