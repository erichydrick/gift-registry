package server_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/net/html"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
)

var (
	dbPath string
)

func init() {
	dbPath = filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")
}

func TestLoginEmailValidationForm(t *testing.T) {

	testData := []struct {
		email              string
		expectedEmailSent  bool
		expectedFields     map[string]bool
		expectedStatusCode int
		testName           string
	}{
		{
			email:             test.ValidEmail,
			expectedEmailSent: true,
			expectedFields: map[string]bool{
				"verify-code":       true,
				"verify-code-error": false,
				"verify-email":      false,
				"verify-error":      false},
			expectedStatusCode: 200,
			testName:           "Valid email",
		},
		{
			email:             test.OutsideEmail,
			expectedEmailSent: false,
			expectedFields: map[string]bool{
				"verify-code":       true,
				"verify-code-error": false,
				"verify-email":      false,
				"verify-error":      false,
			},
			expectedStatusCode: 200,
			testName:           "Invalid user",
		},
		{
			email:             "no",
			expectedEmailSent: false,
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": true,
				"login-error":       false,
			},
			expectedStatusCode: 200,
			testName:           "Invalid email",
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

			err = test.CreateUser(ctx, db)
			if err != nil {
				t.Fatal("Error creating a person record to use for testing", err)
			}

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, db, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			form := url.Values{}
			form.Add("email", data.email)

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
			if emailer.(*test.EmailMock).VerificationEmailSent != data.expectedEmailSent {

				t.Fatalf("Should the verification email have been sent? (%v) Was it? (%v)",
					data.expectedEmailSent,
					emailer.(*test.EmailMock).VerificationEmailSent,
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
		{
			expectedStatus: 500,
			envOverrides:   map[string]string{"TEMPLATES_DIR": "."},
			testName:       "Bad Template",
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

			/*
				Override environment values with test-specific ones if needed
			*/
			maps.Copy(env, data.envOverrides)
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
		duration             time.Duration
		email                string
		enteredToken         string
		expectedFields       map[string]bool
		expectedStatusCode   int
		location             string
		testName             string
		token                string
		verificationSuccess  bool
		verifyEmailPopulated bool
	}{
		{
			attempts:     0,
			duration:     -5 * time.Minute,
			email:        test.ValidEmail,
			enteredToken: "expired-token",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": true,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode:   200,
			testName:             "Expired token",
			token:                "expired-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:     server.MaxAttempts + 5,
			duration:     5 * time.Minute,
			email:        test.ValidEmail,
			enteredToken: "thisiswrong",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode:   200,
			token:                "unentered-token",
			testName:             "Failed attempts exceeded",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts: server.MaxAttempts - 1, duration: 5 * time.Minute,
			email:        test.ValidEmail,
			enteredToken: "thisiswrong",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode:   200,
			testName:             "Failed attempts at max",
			token:                "unentered-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:     0,
			duration:     5 * time.Minute,
			email:        test.ValidEmail,
			enteredToken: "thisiswrong",
			expectedFields: map[string]bool{
				"verify-code":  true,
				"verify-email": false,
				"verify-error": true,
				"verify-form":  true,
			},
			expectedStatusCode:   200,
			testName:             "Failed attempts more remaining",
			token:                "unentered-token",
			verifyEmailPopulated: true,
			verificationSuccess:  false,
		},
		{
			attempts:     0,
			duration:     5 * time.Minute,
			email:        test.OutsideEmail,
			enteredToken: "unentered-token",
			expectedFields: map[string]bool{
				"login-email":       true,
				"login-email-error": false,
				"login-form":        true,
				"login-submit":      true,
			},
			expectedStatusCode:   200,
			testName:             "Failed invalid email",
			token:                "unentered-token",
			verifyEmailPopulated: false,
			verificationSuccess:  false,
		},
		{
			attempts:             0,
			duration:             5 * time.Minute,
			email:                test.ValidEmail,
			enteredToken:         "valid-token",
			expectedFields:       map[string]bool{},
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

			err = test.CreateUser(ctx, db)
			if err != nil {
				t.Fatal("Error creating a person record to use for testing", err)
			}

			err = createToken(ctx, db, data.token, data.duration, data.attempts)
			if err != nil {
				t.Fatal("Error creating a verification record to use for testing", err)
			}

			var emailer server.Emailer = &test.EmailMock{}
			appHandler, err := server.NewServer(getenv, db, logger, emailer)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			form := url.Values{}
			form.Add("code", data.enteredToken)
			form.Add("email", data.email)

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
	ctx context.Context,
	db *sql.DB,
	token string,
	duration time.Duration,
	attempts int,
) error {

	expires := time.Now().Add(duration).UTC()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Println("Error starting the create user transaction")
		return fmt.Errorf("error starting transaction to write a test verification record: %v", err)
	}

	/*
		Do the insertion and make sure it worked. We're going to t.Fatal() if this
		fails, so I'm not going to worry about Rollback() calls erroring, the
		database is going to be deleted anyhow
	*/
	if res, err := db.ExecContext(ctx, "INSERT INTO verification (email, token, token_expiration, attempts) VALUES ($1, $2, $3, $4)", test.ValidEmail, token, expires, attempts); err != nil {
		log.Println("Error adding a new test verification record to the database.")
		tx.Rollback()
		return fmt.Errorf("error executing insert operation: %v", err)
	} else if added, err := res.RowsAffected(); err != nil {
		log.Println("Error getting the last inserted ID from the test person creation.")
		tx.Rollback()
		return fmt.Errorf("no rows inserted into the table: %v", err)
	} else if added < 1 {
		log.Println("No rows were added to the verification table!")
		tx.Rollback()
		return fmt.Errorf("did not complete insertion for test verification details: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return err
	}

	return nil

}
