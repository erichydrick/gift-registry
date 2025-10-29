package middleware_test

import (
	"gift-registry/internal/middleware"
	"gift-registry/internal/test"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestAuthMiddleware(t *testing.T) {

	testData := []struct {
		createSession  bool
		elements       map[string]test.ElementValidation
		email          string
		expectedStatus int
		firstName      string
		lastName       string
		path           string
		sessionAgent   string
		testName       string
		timeLeft       time.Duration
		userAgent      string
		validSession   bool
	}{
		{
			createSession: false,
			elements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-submit":      {Visible: true},
				"login-email-error": {Visible: false}},
			email:          "unprotectedEndpointTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Unprotected",
			lastName:       "Endpoint",
			path:           "/login",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unprotected endpoint",
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			createSession: false,
			elements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-submit":      {Visible: true},
				"login-email-error": {Visible: false},
			},
			email:          "protectedEndpointNoCookieTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Protected",
			lastName:       "Endpoint",
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Protected endpoint no cookie",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession: true,
			elements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-submit":      {Visible: true},
				"login-email-error": {Visible: false},
			},
			email:          "idNotInDBTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Idnot",
			lastName:       "Indb",
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access ID not in DB",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   false,
		},
		{
			createSession: true,
			elements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-submit":      {Visible: true},
				"login-email-error": {Visible: false},
			},
			email:          "sessionExpiredTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Session",
			lastName:       "Expired",
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access session expired",
			timeLeft:       -1 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession: true,
			elements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-submit":      {Visible: true},
				"login-email-error": {Visible: false},
			},
			email:          "wrongUserAgentTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Wrong",
			lastName:       "Agent",
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Unauthorized access wrong user agent",
			timeLeft:       5 * time.Minute,
			userAgent:      "nottherightuseragent",
			validSession:   true,
		},
		{
			createSession: true,
			elements: map[string]test.ElementValidation{
				"registry-data": {Visible: true},
			},
			email:          "validSessionTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Valid",
			lastName:       "Session",
			path:           "/registry",
			sessionAgent:   test.DefaultUserAgent,
			testName:       "Valid session",
			timeLeft:       5 * time.Minute,
			userAgent:      test.DefaultUserAgent,
			validSession:   true,
		},
		{
			createSession: true,
			elements: map[string]test.ElementValidation{
				"registry-data": {Visible: true},
			},
			email:          "loginTest@localhost.com",
			expectedStatus: http.StatusOK,
			firstName:      "Login",
			lastName:       "User",
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

			sessCookie := http.Cookie{}

			if data.createSession {

				userData := test.UserData{
					Email:     data.email,
					FirstName: data.firstName,
					LastName:  data.lastName,
				}

				sessionID, err := test.CreateSession(ctx, logger, db, userData, data.timeLeft, data.sessionAgent)
				if err != nil {
					t.Fatal("Error setting up test session", err)
				}

				sessCookie.Name = middleware.SessionCookie
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

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing the HTML response", err)
			}

			err = test.ValidatePage(doc, data.elements)
			if err != nil {
				t.Fatal("Page validation failed:", err)
			}

		})

	}
}
