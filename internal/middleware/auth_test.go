package middleware_test

import (
	"net/http"
	"testing"
	"time"

	"gift-registry/internal/middleware"
	"gift-registry/internal/test"

	"golang.org/x/net/html"
)

func TestAuthMiddleware(t *testing.T) {
	testData := []struct {
		elementsFile   string
		expectedStatus int
		path           string
		sfDest         string
		sfMode         string
		sfSite         string
		testName       string
		token          string
		userAgent      string
		validSession   bool
	}{
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/login",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Unprotected endpoint",
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/registry",
			testName:       "Protected endpoint no cookie",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Unauthorized access ID not in DB",
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Unauthorized access session expired",
			token:          "expired-session-token",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Unauthorized access wrong user agent",
			token:          "wrong-agent-token",
			userAgent:      "nottherightuseragent",
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_registry_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/registry",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Valid session",
			token:          "protected-endpoint-access",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/login",
			sfDest:         "test document",
			sfMode:         "same-origin",
			sfSite:         "same-origin",
			testName:       "Invalid Sec-Fetch-Dest",
			token:          "protected-endpoint-access",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/login",
			sfDest:         "document",
			sfMode:         "haxxoring",
			sfSite:         "same-origin",
			testName:       "Invalid Sec-Fetch-Mode",
			token:          "protected-endpoint-access",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			elementsFile:   "middleware_auth_login_page_elements.json",
			expectedStatus: http.StatusOK,
			path:           "/login",
			sfDest:         "document",
			sfMode:         "same-origin",
			sfSite:         "evil site, inc",
			testName:       "Invalid Sec-Fetch-Site",
			token:          "protected-endpoint-access",
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			sessCookie := http.Cookie{}

			if data.token != "" {

				sessCookie.Name = middleware.SessionCookie
				sessCookie.MaxAge = time.Now().UTC().Add(5 * time.Minute).Second()
				sessCookie.HttpOnly = true
				sessCookie.Secure = true
				sessCookie.SameSite = http.SameSiteStrictMode

				if data.validSession {
					sessCookie.Value = data.token
				} else {
					sessCookie.Value = "Invalid Session ID"
				}

			}

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+data.path, nil)
			if err != nil {
				t.Fatal("Error submitting the form to the server!", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", data.userAgent)
			req.Header.Set("Sec-Fetch-Dest", data.sfDest)
			req.Header.Set("Sec-Fetch-Mode", data.sfMode)
			req.Header.Set("Sec-Fetch-Site", data.sfSite)
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

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing the HTML response", err)
			}

			expectedElements, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected elements", err)
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal("Page validation failed:", err)
			}
		})
	}
}
