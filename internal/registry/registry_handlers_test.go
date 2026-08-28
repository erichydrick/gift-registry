package registry_test

import (
	"context"
	"gift-registry/internal/database"
	"gift-registry/internal/middleware"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/html"
)

const (
	dbName    = "registry_test"
	userAgent = "test-user-agent"
)

var (
	ctx                  context.Context
	db                   database.Database
	expectedElementsPath string
	getenv               func(string) string
	logger               *slog.Logger
	testServer           *httptest.Server
)

func TestMain(m *testing.M) {

	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	env := map[string]string{
		"DATA_MIGRATIONS":  filepath.Join("..", "..", "testing_data", "registry_data", "sql"),
		"DB_NAME":          dbPath,
		"MIGRATIONS_DIR":   filepath.Join("..", "..", "internal", "database", "migrations"),
		"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
		"TEMPLATES_DIR":    filepath.Join("..", "..", "cmd", "web", "templates"),
	}
	getenv = func(name string) string { return env[name] }

	db, err = database.Connect(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	appHandler, err := server.NewServer(getenv, db, logger, nil)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testServer = httptest.NewServer(appHandler)
	defer testServer.Close()

	expectedElementsPath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "registry_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not load the path to the expected outputs directory", err)
	}

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)

}

func TestRegistryPage(t *testing.T) {

	testData := []struct {
		elementsFile string
		testName     string
		token        string
	}{
		{
			elementsFile: "success_registry_display_page.json",
			testName:     "Successful registry view",
			token:        "mom-registry-session",
		},
		{
			elementsFile: "success_registry_display_page_other_person.json",
			testName:     "Can view claim details for other households",
			token:        "grandma-registry-session",
		},
		{
			elementsFile: "success_registry_display_page_other_person.json",
			testName:     "Can see claimants for other household member's gifts",
			token:        "dad-registry-session",
		},
	}
	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    data.token,
			}

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/registry", nil)
			if err != nil {
				t.Fatal("Error building registry request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")

			res, err := http.DefaultClient.Do(req)
			defer func() {
				_ = res.Body.Close()
			}()
			if err != nil {
				t.Fatal("Error getting the registry page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Expected a success status from the server, but got", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			expectedElements, err := test.LoadExpectedElements(expectedElementsPath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load the list of elements to validate!", err)
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal("Output validation failed!", err)
			}

		})

	}

}
