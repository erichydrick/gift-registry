package server_test

import (
	"context"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

// Test-specific values
var (
	ctx        context.Context
	db         database.Database
	dbPath     string
	emailer    server.Emailer
	getenv     func(string) string
	logger     *slog.Logger
	testServer *httptest.Server
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	ctx = context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	dbPath = filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")
	dbCont, dbURL, err := test.BuildDBContainer(ctx, dbPath, dbName, dbUser, dbPass)
	defer func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}()
	if err != nil {
		log.Fatal("Error setting up a test database", err)
	}

	env := map[string]string{
		"DB_HOST":          strings.Split(dbURL, ":")[0],
		"DB_USER":          dbUser,
		"DB_PASS":          dbPass,
		"DB_PORT":          strings.Split(dbURL, ":")[1],
		"DB_NAME":          dbName,
		"MIGRATIONS_DIR":   filepath.Join("..", "..", "internal", "database", "migrations"),
		"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
		"TEMPLATES_DIR":    filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(name string) string { return env[name] }

	db, err = database.Connection(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	emailer = &test.EmailMock{
		EmailToToken: map[string]string{},
		EmailToSent:  map[string]bool{},
	}
	appHandler, err := server.NewServer(getenv, db, logger, emailer)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testServer = httptest.NewServer(appHandler)
	defer testServer.Close()

	exitCode := m.Run()
	os.Exit(exitCode)

}

// Confirms we get a 500 bad response if we have any error reading or populating a template, simulated by intentionally misconfiguring the templates directory.
func TestBadTemplates(t *testing.T) {

	testData := []struct {
		formData   url.Values
		httpMethod string
		path       string
		testName   string
	}{
		{
			formData: url.Values{
				"email": []string{"testLoginSubmit@localhost.com"},
			},
			httpMethod: "POST",
			path:       "/login",
			testName:   "Login Submit",
		},
		{
			formData:   url.Values{},
			httpMethod: "GET",
			path:       "/login",
			testName:   "Login Form",
		},
		{
			formData: url.Values{
				"code":  []string{"testCode"},
				"email": []string{"verificationTest@localhost.com"},
			},
			httpMethod: "POST",
			path:       "/verify",
			testName:   "Verification",
		},
		{
			formData:   url.Values{},
			httpMethod: "GET",
			path:       "/",
			testName:   "Index Page",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			env := map[string]string{
				"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
				"TEMPLATES_DIR":    "templates",
			}
			getenv := func(name string) string { return env[name] }

			appHandler, err := server.NewServer(getenv, db, logger, emailer)
			if err != nil {
				log.Fatal("Error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, data.httpMethod, testServer.URL+data.path, strings.NewReader(data.formData.Encode()))
			if err != nil {
				t.Fatal("Error submitting the form to the server!", err)
			}

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error reading the response from email validation!", err)
			}

			if res.StatusCode != http.StatusInternalServerError {

				t.Fatal("Expected a 500 response")

			}

			// TODO: VERIFY I HAVE A MESSAGE IN HERE?

		})

	}

}
