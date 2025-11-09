package server_test

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
)

func TestLoginEmailValidationForm(t *testing.T) {

	testData := []struct {
		expectedEmailSent  bool
		expectedFields     map[string]bool
		expectedStatusCode int
		testName           string
		userData           test.UserData
	}{
		{
			expectedEmailSent: true,
			expectedFields: map[string]bool{
				"verify-code":       true,
				"verify-code-error": false,
				"verify-email":      false,
				"verify-error":      false},
			expectedStatusCode: 200,
			testName:           "Valid email",
			userData: test.UserData{
				DisplayName: "Allgood",
				Email:       "validemailtest@localhost.com",
				FirstName:   "Valid",
				LastName:    "Email",
			},
		},
		{
			expectedEmailSent: false,
			expectedFields: map[string]bool{
				"verify-code":       true,
				"verify-code-error": false,
				"verify-email":      false,
				"verify-error":      false,
			},
			expectedStatusCode: 200,
			testName:           "Invalid user",
			userData: test.UserData{
				DisplayName: "Whoami",
				Email:       "unregistereduser@localhost.com",
				FirstName:   "Unregistered",
				LastName:    "User",
			},
		},
		{
			expectedEmailSent: false,
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": true,
				"login-error":       false,
			},
			expectedStatusCode: 200,
			testName:           "Invalid email",
			userData: test.UserData{
				Email:     "no",
				FirstName: "John",
				LastName:  "Dont",
			},
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			/*
				Pre-populate the database with a registered user email if needed.
			*/
			if data.expectedEmailSent {

				_, err := test.CreateUser(ctx, logger, db, data.userData)
				if err != nil {
					t.Fatal("Error creating a person record to use for testing", err)
				}

			}

			form := url.Values{}
			form.Add("email", data.userData.Email)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/login", strings.NewReader(form.Encode()))
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

			if res.StatusCode != data.expectedStatusCode {

				t.Fatal("Expected a response status of", data.expectedStatusCode, "but got", res.StatusCode)

			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing reponse body")
			}

			for id, visible := range data.expectedFields {

				if pageElem, ok := test.CheckElement(*doc, id); ok == false {

					t.Fatal("Could not find element", id, "on the page")

				} else if elemVis := test.ElementVisible(pageElem); elemVis != test.ElementVisible(pageElem) {

					t.Fatal("Expected element", id, "to have visibility =", visible, "but it was", elemVis)

				}
			}

			/*
				We could have triggered more than 1 email because we tested agaisnt more
				than 1 browser.
			*/
			if sent, ok := emailer.(*test.EmailMock).EmailToSent[data.userData.Email]; ok && sent != data.expectedEmailSent {

				t.Fatalf("Should the verification email have been sent? (%v) Was it? (%v)",
					data.expectedEmailSent,
					emailer.(*test.EmailMock).EmailToSent[data.userData.Email],
				)

			}

		})

	}

}

func TestLoginForm(t *testing.T) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	testData := []struct {
		expectedStatus   int
		expectedElements map[string]bool
		envOverrides     map[string]string
		hiddenElements   []string
		testName         string
	}{
		{
			expectedStatus: 200,
			expectedElements: map[string]bool{
				"application-header": true,
				"login-email":        true,
				"login-email-error":  false,
				"login-error":        false,
				"login-form":         true,
			},
			testName: "Success",
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
					res.Body.Close()
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

			for id, visible := range data.expectedElements {

				if pageElem, ok := test.CheckElement(*doc, id); ok == false {

					t.Fatal("Could not find element", id, "on the page")

				} else if elemVis := test.ElementVisible(pageElem); elemVis != test.ElementVisible(pageElem) {

					t.Fatal("Expected element", id, "to have visibility =", visible, "but it was", elemVis)

				}
			}

		})

	}

}

func TestVerification(t *testing.T) {

	testData := []struct {
		attempts             int
		createSession        bool
		duration             time.Duration
		enteredToken         string
		expectedFields       map[string]bool
		expectedStatusCode   int
		location             string
		testName             string
		token                string
		userData             test.UserData
		verificationSuccess  bool
		verifyEmailPopulated bool
	}{
		{
			attempts:      0,
			createSession: true,
			duration:      -5 * time.Minute,
			enteredToken:  "expired-token",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": true,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode: 200,
			testName:           "Expired token",
			token:              "expired-token",
			userData: test.UserData{
				Email:     "expiredTokenTest@localhost.com",
				FirstName: "Expired",
				LastName:  "Token",
			},
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:      server.MaxAttempts + 5,
			createSession: true,
			duration:      5 * time.Minute,
			enteredToken:  "thisiswrong",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode: 200,
			token:              "unentered-exceeded-token",
			userData: test.UserData{
				Email:     "exceededAttemptsTokenTest@localhost.com",
				FirstName: "Exceeded",
				LastName:  "Attempts",
			},
			testName:             "Failed attempts exceeded",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts: server.MaxAttempts - 1, duration: 5 * time.Minute,
			createSession: true,
			enteredToken:  "thisiswrong",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode: 200,
			testName:           "Failed attempts at max",
			token:              "unentered-max-token",
			userData: test.UserData{
				Email:     "maxedFailuresTokenTest@localhost.com",
				FirstName: "Maxed",
				LastName:  "Failures",
			},
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:      0,
			createSession: true,
			duration:      5 * time.Minute,
			enteredToken:  "thisiswrong",
			expectedFields: map[string]bool{
				"verify-code":  true,
				"verify-email": false,
				"verify-error": true,
				"verify-form":  true,
			},
			expectedStatusCode: 200,
			testName:           "Failed attempts more remaining",
			token:              "unentered-more-tries-token",
			userData: test.UserData{
				Email:     "moreTriesTokenTest@localhost.com",
				FirstName: "More",
				LastName:  "Tries",
			},
			verifyEmailPopulated: true,
			verificationSuccess:  false,
		},
		{
			attempts:      0,
			createSession: false,
			duration:      5 * time.Minute,
			enteredToken:  "invalid-email-token",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode: 200,
			testName:           "Failed invalid email",
			token:              "invalid-email-token",
			userData: test.UserData{
				Email:     "invalidEmail@localhost.com",
				FirstName: "Invalid",
				LastName:  "Email",
			},
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:           0,
			createSession:      true,
			duration:           5 * time.Minute,
			enteredToken:       "valid-token",
			expectedFields:     map[string]bool{},
			expectedStatusCode: http.StatusOK,
			location:           "/registry",
			testName:           "Successful verification",
			token:              "valid-token",
			userData: test.UserData{
				Email:     "successfulVerification@localhost.com",
				FirstName: "Successful",
				LastName:  "Verification",
			},
			verifyEmailPopulated: false,
			verificationSuccess:  true,
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			if data.createSession {

				log.Println("Need to create a user and associated token", data.userData, data.token, data.duration, data.attempts)
				err := createToken(db, data.userData, data.token, data.duration, data.attempts)
				if err != nil {
					t.Fatal("Error creating a verification record to use for testing: ", err)
				}
				log.Println("User and associated token created successfully")

			}

			form := url.Values{}
			form.Add("code", data.enteredToken)
			form.Add("email", data.userData.Email)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/verify", strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal("Error submitting the form to the server!", err)
			}

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res.Body != nil {
					res.Body.Close()
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

			for id, visible := range data.expectedFields {

				if pageElem, ok := test.CheckElement(*doc, id); ok == false {

					t.Fatal("Could not find element", id, "on the page")

				} else if elemVis := test.ElementVisible(pageElem); elemVis != test.ElementVisible(pageElem) {

					t.Fatal("Expected element", id, "to have visibility =", visible, "but it was", elemVis)

				}
			}

		})

	}

}

func createToken(
	dbConn database.Database,
	userData test.UserData,
	token string,
	duration time.Duration,
	attempts int,
) error {

	personID, err := test.CreateUser(ctx, logger, dbConn, userData)
	if err != nil {
		return fmt.Errorf("error creating a test user to associate with the verification token: %v", err)
	}

	expires := time.Now().Add(duration).UTC()

	/*
		Do the insertion and make sure it worked. We're going to t.Fatal() if this
		fails, so I'm not going to worry about Rollback() calls erroring, the
		database is going to be deleted anyhow
	*/
	if res, err := dbConn.Execute(ctx, "INSERT INTO verification (person_id, token, token_expiration, attempts) VALUES ($1, $2, $3, $4)", personID, token, expires, attempts); err != nil {
		log.Println("Error adding a new test verification record to the database.")
		return fmt.Errorf("error executing insert operation: %v", err)
	} else if added, err := res.RowsAffected(); err != nil {
		log.Println("Error getting the last inserted ID from the test person creation.")
		return fmt.Errorf("no rows inserted into the table: %v", err)
	} else if added < 1 {
		log.Println("No rows were added to the verification table!")
		return fmt.Errorf("did not complete insertion for test verification details: %v", err)
	}

	logger.DebugContext(ctx, "Single verification record added!")

	return nil

}
