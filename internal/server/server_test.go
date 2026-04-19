package server_test

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
)

// Connection details for the test database
const (
	dbName = "server_test_database"
)

// Test-specific values
var (
	ctx        context.Context
	db         database.Database
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

	srcDB, err := filepath.Abs(filepath.Join("..", "test", "test.db"))
	if err != nil {
		log.Fatal("Could not find test database source: ", err)
	}

	dbPath, err := filepath.Abs(filepath.Join(dbName))
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

	env := map[string]string{
		"DB_NAME":        dbPath,
		"MIGRATIONS_DIR": filepath.Join("..", "database", "migrations"),
		"TEMPLATES_DIR":  filepath.Join("..", "..", "cmd", "web", "templates"),
	}

	getenv = func(name string) string { return env[name] }

	db, err = database.Connect(ctx, logger, getenv)
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

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

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
				t.Fatal("Error reading the response from email validation!", err)
			}

			if res.StatusCode != http.StatusInternalServerError {
				t.Fatal("Expected a 500 response")
			}
		})
	}
}
