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
		email                 string
		expectedEmailSent     bool
		expectedHiddenFields  []string
		expectedStatusCode    int
		expectedVisibleFields []string
		testName              string
	}{
		{email: test.ValidEmail, expectedEmailSent: true, expectedHiddenFields: []string{"verify-email", "verify-code-error", "verify-error"}, expectedVisibleFields: []string{"verify-code"}, expectedStatusCode: 200, testName: "Valid email"},
		{email: test.OutsideEmail, expectedEmailSent: false, expectedHiddenFields: []string{"verify-email", "verify-code-error", "verify-error"}, expectedVisibleFields: []string{"verify-code"}, expectedStatusCode: 200, testName: "Invalid user"},
		{email: "no", expectedEmailSent: false, expectedHiddenFields: []string{"login-error"}, expectedVisibleFields: []string{"login-email-error", "login-email"}, expectedStatusCode: 200, testName: "Invalid email"},
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

				for _, elemID := range data.expectedVisibleFields {

					locator := page.Locator("#" + elemID)

					if visible, err := locator.IsVisible(); !visible || err != nil {
						t.Fatal("Could not find expected element", "#"+elemID, "in", bType.Name())
					}

				}

				for _, elemID := range data.expectedHiddenFields {

					locator := page.Locator("#" + elemID)
					found, err := locator.Count()
					if err != nil {
						t.Fatal("Error trying to locate", "#"+elemID)
					} else if found == 0 {
						t.Fatal("Expected hidden element", "#"+elemID, "not found! Should be on page but hidden")
					}

					visible, err := locator.IsVisible()
					if err != nil {
						t.Fatal("Error trying to checking visibility of", "#"+elemID)
					} else if visible {
						t.Fatal("Expected element", "#"+elemID, "to be hidden on the page")
					}

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
		expectedElements []string
		envOverrides     map[string]string
		hiddenElements   []string
		testName         string
	}{
		{expectedStatus: 200, expectedElements: []string{"application-header", "login-form", "login-email"}, hiddenElements: []string{"login-email-error", "login-error"}, testName: "Success"},
		{expectedStatus: 500, envOverrides: map[string]string{"TEMPLATES_DIR": "."}, testName: "Bad Template"},
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

			for _, bType := range browsers {

				page, err := test.GetPage(bType)
				if err != nil {
					t.Fatalf("Error creating new webpage object %v", err)
				}
				_, err = page.Evaluate("let htmx = window.htmx")
				if err != nil {
					t.Fatal("Error evaluating HTMX afterSettle!", err)
				}

				_, err = page.Goto(testServer.URL + "/login")
				if err != nil {
					t.Fatalf("Error opening the page %v", err)
				}

				for _, elemID := range data.expectedElements {

					locator := page.Locator("#" + elemID)

					if visible, err := locator.IsVisible(); !visible || err != nil {
						t.Fatal("Could not find expected element", "#"+elemID, "in", bType.Name())
					}

				}

				for _, elemID := range data.hiddenElements {

					locator := page.Locator("#" + elemID)
					found, err := locator.Count()
					if err != nil {
						t.Fatal("Error trying to locate", "#"+elemID)
					} else if found == 0 {
						t.Fatal("Expected hidden element", "#"+elemID, "not found! Should be on page but hidden")
					}

					visible, err := locator.IsVisible()
					if err != nil {
						t.Fatal("Error trying to checking visibility of", "#"+elemID)
					} else if visible {
						t.Fatal("Expected element", "#"+elemID, "to be hidden on the page")
					}

				}

			}

		})

	}

}

func TestVerification(t *testing.T) {

	testData := []struct {
		attempts              int
		duration              time.Duration
		email                 string
		enteredToken          string
		expectedVisibleFields []string
		expectedHiddenFields  []string
		expectedStatusCode    int
		location              string
		testName              string
		token                 string
		verificationSuccess   bool
		verifyEmailPopulated  bool
	}{
		{attempts: 0, duration: -5 * time.Minute, email: test.ValidEmail, enteredToken: "expired-token", expectedVisibleFields: []string{"login-form", "login-email", "login-submit"}, expectedHiddenFields: []string{"login-email-error"}, token: "expired-token", expectedStatusCode: 200, testName: "Expired token", verificationSuccess: false, verifyEmailPopulated: false},
		{attempts: server.MaxAttempts + 5, duration: 5 * time.Minute, email: test.ValidEmail, enteredToken: "thisiswrong", expectedVisibleFields: []string{"login-form", "login-email", "login-submit"}, expectedHiddenFields: []string{"login-email-error"}, token: "unentered-token", expectedStatusCode: 200, testName: "Failed attempts exceeded", verificationSuccess: false, verifyEmailPopulated: false},
		{attempts: server.MaxAttempts - 1, duration: 5 * time.Minute, email: test.ValidEmail, enteredToken: "thisiswrong", expectedVisibleFields: []string{"login-form", "login-email", "login-submit"}, expectedHiddenFields: []string{"login-email-error"}, token: "unentered-token", expectedStatusCode: 200, testName: "Failed attempts at max", verificationSuccess: false, verifyEmailPopulated: false},
		{attempts: 0, duration: 5 * time.Minute, email: test.ValidEmail, enteredToken: "thisiswrong", expectedVisibleFields: []string{"verify-form", "verify-error", "verify-code"}, expectedHiddenFields: []string{"verify-email"}, token: "unentered-token", expectedStatusCode: 200, testName: "Failed attempts more remaining", verificationSuccess: false, verifyEmailPopulated: true},
		{attempts: 0, duration: 5 * time.Minute, email: test.OutsideEmail, enteredToken: "unentered-token", expectedVisibleFields: []string{"login-form", "login-email", "login-submit"}, expectedHiddenFields: []string{"login-email-error"}, token: "unentered-token", expectedStatusCode: 200, testName: "Failed invalid email", verificationSuccess: false, verifyEmailPopulated: false},
		{attempts: 0, duration: 5 * time.Minute, email: test.ValidEmail, enteredToken: "valid-token", expectedVisibleFields: []string{}, expectedHiddenFields: []string{}, token: "valid-token", expectedStatusCode: http.StatusOK, location: "/registry", testName: "Successful verification", verificationSuccess: true, verifyEmailPopulated: false},
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
			res := httptest.NewRecorder()
			appHandler.ServeHTTP(res, req)

			if res.Result().StatusCode != data.expectedStatusCode {

				t.Fatal("Expected a response status of", data.expectedStatusCode, "but got", res.Result().StatusCode)

			}

			if res.Code == http.StatusSeeOther {

				if res.Result().Header.Get("Location") != data.location {
					t.Fatal("Expected", data.location, "but redirected to", res.Result().Header.Get("Location"))
				}

			}

			for _, bType := range browsers {

				page, err := test.GetPage(bType)
				if err != nil {
					t.Fatal("Error getting a ", bType.Name(), "browser page!")
				}

				err = page.SetContent(res.Body.String())
				if err != nil {
					t.Fatal("Error loading up the page content!")
				}

				/* Look for elements a user should be seeing */
				for _, elemID := range data.expectedVisibleFields {

					locator := page.Locator("#" + elemID)

					if visible, err := locator.IsVisible(); !visible || err != nil {
						t.Fatal("Could not find expected element", "#"+elemID, "in", bType.Name())
					}

				}

				/* There are some elements that should be on the page, but hidden */
				for _, elemID := range data.expectedHiddenFields {

					locator := page.Locator("#" + elemID)
					if found, err := locator.Count(); err != nil {
						t.Fatal("Error trying to locate", "#"+elemID)
					} else if found == 0 {
						t.Fatal("Expected hidden element", "#"+elemID, "not found! Should be on page but hidden")
					}

					if visible, err := locator.IsVisible(); err != nil {
						t.Fatal("Error trying to checking visibility of", "#"+elemID)
					} else if visible {
						t.Fatal("Expected element", "#"+elemID, "to be hidden on the page")
					}

				}

				/* Verify we're on a valid verification form */
				if data.verifyEmailPopulated {

					locator := page.Locator("#verify-email")

					if found, err := locator.Count(); err != nil {
						t.Fatal("Error trying to locate the verify email field")
					} else if found == 0 {
						t.Fatal("Expected the verify email field to be present")
					}

					if value, err := locator.InputValue(); err != nil {
						t.Fatal("Error confirming the verify email field to have a value")
					} else if value != data.email {
						t.Fatal("Expected the verify email field to have a value, even if it's hidden!")
					}

					if visible, err := locator.IsVisible(); err != nil {
						t.Fatal("Error trying to checking visibility of the verify-email field")
					} else if visible {
						t.Fatal("Expected the verify email field to be hidden")
					}

					/* There should still be a record in the verification table */
					if rows, err := db.Query("SELECT * FROM verification WHERE email = $1", data.email); err != nil {
						t.Fatal("Error checking the verification table for the email", err)
					} else if !rows.Next() {
						t.Fatal("Expected a verification table record under email", data.email)
					}

				}

				/* Check for a session record */
				if data.verificationSuccess {

					/*
						There should now be a record in the session table.
						No this isn't indexed, but there's 1 record in the table, so I don't care.
					*/
					if rows, err := db.Query("SELECT * FROM session WHERE email = $1", data.email); err != nil {
						t.Fatal("Error checking the session table for the email", err)
					} else if !rows.Next() {
						t.Fatal("Expected a verification table record under email", data.email)
					}

					/*
						Verify the session cookie exists
					*/
					validSessionCookie := false
					var sessionCookie *http.Cookie
					for _, cookie := range res.Result().Cookies() {
						if cookie.Name != server.SessionCookie {
							continue
						}
						/* Found the session cookie, check it out */
						sessionCookie = cookie
						validSessionCookie = true && cookie.HttpOnly && cookie.Secure
						validSessionCookie = validSessionCookie && cookie.Value != ""
						validSessionCookie = validSessionCookie && cookie.MaxAge > int(4*time.Minute.Seconds()) && cookie.MaxAge < int(5*time.Minute.Seconds())
						break
					}

					if !validSessionCookie {
						t.Fatalf("Expected a valid session cookie: %v", sessionCookie)
					}

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
