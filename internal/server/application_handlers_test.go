package server_test

import (
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
	"golang.org/x/net/html"
)

func TestAuthMiddleware(t *testing.T) {

	browsers, err := test.BrowserList()
	if err != nil {
		t.Fatal("Error building browser list", err)
	}

	testData := []struct {
		createSession  bool
		elements       map[string]bool
		expectedStatus int
		path           string
		sessionAgent   string
		testName       string
		timeLeft       time.Duration
		userAgent      string
		validSession   bool
	}{
		{
			createSession:  false,
			elements:       map[string]bool{"login-form": true, "login-email": true, "login-submit": true, "login-email-error": false},
			expectedStatus: http.StatusOK,
			path:           "/login",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unprotected endpoint",
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			createSession:  false,
			elements:       map[string]bool{"login-form": true, "login-email": true, "login-submit": true, "login-email-error": false},
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Protected endpoint no cookie",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession:  true,
			elements:       map[string]bool{"login-form": true, "login-email": true, "login-submit": true, "login-email-error": false},
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access ID not in DB",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			createSession:  true,
			elements:       map[string]bool{"login-form": true, "login-email": true, "login-submit": true, "login-email-error": false},
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access session expired",
			timeLeft:       -1 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession:  true,
			elements:       map[string]bool{"login-form": true, "login-email": true, "login-submit": true, "login-email-error": false},
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access wrong user agent",
			timeLeft:       5 * time.Minute,
			userAgent:      "nottherightuseragent",
			validSession:   true,
		},
		{
			createSession:  true,
			elements:       map[string]bool{"registry-data": true},
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Valid session",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession:  true,
			elements:       map[string]bool{"registry-data": true},
			expectedStatus: http.StatusOK,
			path:           "/login",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Valid session via login",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			dbCont, dbURL, err := test.BuildDBContainer(ctx, dbPath, dbName, dbUser, dbPass)
			defer func() {
				if err := testcontainers.TerminateContainer(dbCont); err != nil {
					log.Fatal("Failed to terminate the database test container ", err)
				}
			}()
			if err != nil {
				t.Fatal("Error setting up a test database", err)
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
			getenv := func(name string) string { return env[name] }

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			sessCookie := http.Cookie{}

			if data.createSession {

				err = test.CreateUser(ctx, db)
				if err != nil {
					t.Fatal("Error setting up a user for session testing", err)
				}

				sessionID, err := test.CreateSession(ctx, db, data.timeLeft, data.sessionAgent)
				if err != nil {
					t.Fatal("Error setting up test session", err)
				}

				sessCookie.Name = server.SessionCookie
				sessCookie.MaxAge = time.Now().UTC().Add(data.timeLeft).Second()
				sessCookie.HttpOnly = true
				sessCookie.Secure = true
				sessCookie.SameSite = http.SameSiteStrictMode

				if data.validSession {
					sessCookie.Value = sessionID
				} else {
					sessCookie.Value = "Invalid Session ID"
				}

			}

			appHandler, err := server.NewServer(getenv, db, logger, &test.EmailMock{})
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+data.path, nil)
			if err != nil {
				t.Fatal("Error submitting the form to the server!", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", data.userAgent)
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error making request to validate the authorization middleware", err)
			}

			if res.StatusCode != data.expectedStatus {
				t.Fatal("Expected a status of ", data.expectedStatus, "but got", res.StatusCode)
			}

			pgData := test.ReadResult(res)
			for _, bType := range browsers {

				page, err := test.GetPage(bType)
				if err != nil {
					t.Fatal("Error getting a ", bType.Name(), "browser page!")
				}

				err = page.SetContent(string(pgData))
				if err != nil {
					t.Fatal("Error loading up the page content!")
				}

				for elemID, visible := range data.elements {

					locator := page.Locator("#" + elemID)

					if elemVis, err := locator.IsVisible(); err != nil {
						t.Fatal("Error confirming element and its visibility", err)
					} else if elemVis != visible {
						t.Fatal("Expected element #", elemID, "to have visibility of ", visible, "but it was", elemVis)
					}

				}

			}

		})

	}
}

func TestIndexHandler(t *testing.T) {

	testData := []struct {
		expectedElements []string
		expectedStatus   int
		templatesDir     string
		testName         string
	}{
		{expectedElements: []string{"application-header", "page-content", "redirector"}, expectedStatus: 200, templatesDir: "../../cmd/web/templates", testName: "Success"},
		{expectedElements: []string{}, expectedStatus: 500, templatesDir: "templates", testName: "Bad Templates"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			/* Sets up a testing logger */
			options := &slog.HandlerOptions{Level: slog.LevelDebug}
			handler := slog.NewTextHandler(os.Stderr, options)
			logger = slog.New(handler)

			dbCont, dbUrl, err := test.BuildDBContainer(ctx, filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql"), dbName, dbUser, dbPass)
			defer func() {
				if err := testcontainers.TerminateContainer(dbCont); err != nil {
					log.Fatal("Failed to terminate the database test container ", err)
				}
			}()

			if err != nil {
				log.Fatal("Error making database container", err)
			}

			env := map[string]string{
				"DB_USER":        dbUser,
				"DB_PASS":        dbPass,
				"DB_HOST":        strings.Split(dbUrl, ":")[0],
				"DB_PORT":        strings.Split(dbUrl, ":")[1],
				"DB_NAME":        dbName,
				"PORT":           strconv.Itoa(test.FreePort()),
				"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations"),
				"TEMPLATES_DIR":  data.templatesDir,
			}

			getenv := func(name string) string { return env[name] }

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, db, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
			if err != nil {
				t.Fatal("error building landing page request", err)
			}

			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("server call failed", err)
			}

			if res.StatusCode != data.expectedStatus {

				t.Fatal("Expected a status code of ", data.expectedStatus, " but got ", res.StatusCode)

			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing the HTML content from the response", err)
			}

			for _, elemID := range data.expectedElements {

				elementFound := false

				for node := range doc.Descendants() {

					if node.Attr != nil &&
						slices.Contains(node.Attr, html.Attribute{Key: "id", Val: elemID}) &&
						node.FirstChild.Data != "" {

						elementFound = true
						break

					}

				}

				if !elementFound {

					t.Fatal("Did not find expected element ", elemID)

				}

			}

		})

	}

}
