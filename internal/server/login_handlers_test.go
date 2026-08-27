package server_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"gift-registry/internal/middleware"
	"gift-registry/internal/test"
)

const (
	userAgent = "test-user-agent"
)

func TestLoginEmailValidationForm(t *testing.T) {
	testData := []struct {
		elementsFile       string
		email              string
		expectedEmailSent  bool
		expectedStatusCode int
		testName           string
	}{
		{
			elementsFile:       "verification_page_elements_no_errors.json",
			email:              "validemailtest@localhost.com",
			expectedEmailSent:  true,
			expectedStatusCode: 200,
			testName:           "Valid email",
		},
		{
			elementsFile:       "verification_page_elements_no_errors.json",
			email:              "unregistereduser@localhost.com",
			expectedEmailSent:  false,
			expectedStatusCode: 200,
			testName:           "Invalid user",
		},
		{
			elementsFile:       "login_form_elements_email_error.json",
			email:              "no",
			expectedEmailSent:  false,
			expectedStatusCode: 200,
			testName:           "Invalid email",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			form := url.Values{}
			form.Add("email", data.email)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/login", strings.NewReader(form.Encode()))
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

			if res.StatusCode != data.expectedStatusCode {
				t.Fatal("Expected a response status of", data.expectedStatusCode, "but got", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing reponse body")
			}

			elementsData, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected elements for output validation!", err)
			}

			err = test.ValidatePage(doc, elementsData)
			if err != nil {
				t.Fatal("Output did not conform to expected values!", err)
			}

			if sent, ok := emailer.(*test.EmailMock).EmailToSent[data.email]; ok && sent != data.expectedEmailSent {
				t.Fatalf("Should the verification email have been sent? (%v) Was it? (%v)",
					data.expectedEmailSent,
					emailer.(*test.EmailMock).EmailToSent[data.email],
				)
			}
		})
	}
}

func TestLoginForm(t *testing.T) {

	testData := []struct {
		elementsFile   string
		expectedStatus int
		testName       string
	}{
		{
			elementsFile:   "login_page_elements_no_errors.json",
			expectedStatus: 200,
			testName:       "Success",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/login", nil)
			if err != nil {
				t.Fatal("Error loading the login form page!", err)
			}
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error reading the response from getting the login form!", err)
			}

			if res.StatusCode != data.expectedStatus {
				t.Fatal("Expected a response status of", data.expectedStatus, "but got", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing reponse body")
			}

			elementsData, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected elements for output validation!", err)
			}

			err = test.ValidatePage(doc, elementsData)
			if err != nil {
				t.Fatal("Output did not conform to expected values!", err)
			}
		})
	}
}

func TestVerification(t *testing.T) {
	testData := []struct {
		elementsFile         string
		email                string
		enteredToken         string
		expectedStatusCode   int
		location             string
		testName             string
		token                string
		verificationSuccess  bool
		verifyEmailPopulated bool
	}{
		{
			elementsFile:         "verification_failed_back_to_login.json",
			email:                "expiredTokenTest@localhost.com",
			enteredToken:         "expired-token",
			expectedStatusCode:   200,
			testName:             "Expired token",
			token:                "expired-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			elementsFile:         "verification_failed_back_to_login.json",
			email:                "exceededAttemptsTokenTest@localhost.com",
			enteredToken:         "thisiswrong",
			expectedStatusCode:   200,
			token:                "unentered-exceeded-token",
			testName:             "Failed attempts exceeded",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			elementsFile:         "verification_failed_back_to_login.json",
			email:                "maxedFailuresTokenTest@localhost.com",
			enteredToken:         "thisiswrong",
			expectedStatusCode:   200,
			testName:             "Failed attempts at max",
			token:                "unentered-max-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			elementsFile:         "verification_failed_stay_on_page.json",
			email:                "moreTriesTokenTest@localhost.com",
			enteredToken:         "thisiswrong",
			expectedStatusCode:   200,
			testName:             "Failed attempts more remaining",
			token:                "unentered-more-tries-token",
			verifyEmailPopulated: true,
			verificationSuccess:  false,
		},
		{
			elementsFile:         "verification_failed_back_to_login.json",
			email:                "invalidEmail@localhost.com",
			enteredToken:         "invalid-email-token",
			expectedStatusCode:   200,
			testName:             "Failed invalid email",
			token:                "invalid-email-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			elementsFile:         "registry_page.json",
			email:                "registeredUser@localhost.com",
			enteredToken:         "registered-user-token",
			expectedStatusCode:   http.StatusOK,
			location:             "/registry",
			testName:             "Successful verification",
			token:                "valid-token",
			verifyEmailPopulated: false,
			verificationSuccess:  true,
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			form := url.Values{}
			form.Add("code", data.enteredToken)
			form.Add("email", data.email)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/verify", strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal("Error submitting the form to the server!", err)
			}

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res.Body != nil {
					_ = res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error calling the request endpoint")
			}

			if res.StatusCode != data.expectedStatusCode {
				t.Fatal("Expected a response status of", data.expectedStatusCode, "but got", res.StatusCode)
			}

			if res.StatusCode == http.StatusSeeOther {
				if res.Header.Get("Location") != data.location {
					t.Fatal("Expected", data.location, "but redirected to", res.Header.Get("Location"))
				}
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing the HTML response")
			}

			expectedElements, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected HTML elements from", data.elementsFile, ":", err)
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal("HTML validation failed!", err)
			}

		})
	}
}

func TestLogout(t *testing.T) {
	testData := []struct {
		createSession    bool
		expectedElements map[string]test.ElementValidation
		testName         string
		token            string
	}{
		{
			createSession: true,
			expectedElements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-email-error": {Visible: false},
			},
			testName: "Successful logout",
			token:    "logout-sucess-session",
		},
		{
			createSession: false,
			expectedElements: map[string]test.ElementValidation{
				"login-form":        {Visible: true},
				"login-email":       {Visible: true},
				"login-email-error": {Visible: false},
			},
			testName: "Logout with no session",
			token:    "not-in-database",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 5).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    data.token,
			}

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/logout", nil)
			if err != nil {
				t.Fatal("Could not create HTTP request for logout testing.")
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", userAgent)
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
				t.Fatal("Error making logout request.")
			}

			if res.StatusCode != 200 {
				t.Fatal("Logout failed! Expected a 200, got", res.StatusCode)
			}

			/* Logging out should clear the session cookie. Confirm that. */
			sessCookieReturned := false
			for _, cookie := range res.Cookies() {
				if cookie.Name == middleware.SessionCookie &&
					!cookie.Expires.Before(time.Now()) {

					sessCookieReturned = true
					break

				}
			}
			if sessCookieReturned {
				t.Fatal("Session cookie not cleared out")
			}

			var foundSessionID string
			var foundPersonID int64
			err = db.QueryRow(ctx, "SELECT session_id, person_id FROM session WHERE session_id = ?", data.token).
				Scan(&foundSessionID, foundPersonID)
			if err == nil || err != sql.ErrNoRows {
				t.Fatal("Error confirming logout")
			}

			/* Confirm we loaded the login page */
			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}
			err = test.ValidatePage(doc, data.expectedElements)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
