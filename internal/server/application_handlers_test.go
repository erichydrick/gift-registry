package server_test

import (
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestAuthMiddleware(t *testing.T) {

	start := time.Now()
	browserList, err := test.BrowserList()
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
	log.Println(t.Name(), "- TEST SET UP IN", time.Since(start).Milliseconds(), "MS")

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			sessCookie := http.Cookie{}

			if data.createSession {

				userData := test.UserInfo{
					Email: strings.ReplaceAll(data.testName, " ", "_") + "@authmiddleweartest.com",
				}
				err = test.CreateUser(ctx, db, userData)
				if err != nil {
					t.Fatal("Error setting up a user for session testing", err)
				}

				sessionID, err := test.CreateSession(ctx, db, data.timeLeft, data.sessionAgent, userData)
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
			for _, bType := range browserList {

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

			testEnvs := maps.Clone(env)
			testEnvs["TEMPLATES_DIR"] = data.templatesDir
			getTestEnvs := func(name string) string { return testEnvs[name] }

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getTestEnvs, db, logger, emailer)
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
