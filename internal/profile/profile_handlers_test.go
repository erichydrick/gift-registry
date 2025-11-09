package profile_test

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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/net/html"
)

// Connection details for the test database
const (
	dbName            = "profile_test"
	dbUser            = "profile_user"
	dbPass            = "profile_pass"
	insertPersonQuery = "INSERT INTO person (display_name, email, external_id, first_name, last_name) VALUES ($1, $2, $3, $4, $5)"
	tooLongString     = "12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890"
	userAgent         = "test-user-agent"
)

// Test-specific values
var (
	ctx    context.Context
	db     database.Database
	dbPath string
	/*emailer    server.Emailer*/
	getenv     func(string) string
	logger     *slog.Logger
	testServer *httptest.Server
)

func TestMain(m *testing.M) {

	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	/* Spin up a Postgres container for the tests, and shut it down when done */
	dbPath = filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")
	dbCont, dbURL, err := test.BuildDBContainer(ctx, dbPath, dbName, dbUser, dbPass)
	defer func() {
		if err := testcontainers.TerminateContainer(dbCont); err != nil {
			log.Fatal("Failed to terminate the database test container ", err)
		}
	}()
	if err != nil {
		log.Fatal("Error setting up a test database", err)
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
	getenv = func(name string) string { return env[name] }

	db, err = database.Connection(ctx, logger, getenv)
	if err != nil {
		log.Fatal("database connection failure! ", err)
	}

	appHandler, err := server.NewServer(getenv, db, logger, nil)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testServer = httptest.NewServer(appHandler)
	defer testServer.Close()

	exitCode := m.Run()
	os.Exit(exitCode)

}

func TestProfilePage(t *testing.T) {

	testData := []struct {
		displayName string
		elements    map[string]test.ElementValidation
		email       string
		firstName   string
		lastName    string
		testName    string
	}{
		{
			displayName: "root",
			elements: map[string]test.ElementValidation{
				"profile-header": {
					Value:   "Display Named Profile Page",
					Visible: true,
				},
				"profile-form": {Visible: true},
				"first-name": {
					Value:   "Display",
					Visible: true,
				},
				"last-name": {
					Value:   "Named",
					Visible: true,
				},
				"display-name": {
					Value:   "root",
					Visible: true,
				},
				"email": {
					Value:   "displayName@localhost.com",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: false},
				"last-name-error":  {Visible: false},
				"email-error":      {Visible: false},
				"profile-error":    {Visible: false},
			},
			email:     "displayName@localhost.com",
			firstName: "Display",
			lastName:  "Named",
			testName:  "Successful profile load with display name",
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form": {Visible: true},
				"profile-header": {
					Value:   "Display Nameless Profile Page",
					Visible: true,
				},
				"first-name": {
					Value:   "Display",
					Visible: true,
				},
				"last-name": {
					Value:   "Nameless",
					Visible: true,
				},
				"display-name": {
					Value:   "Display",
					Visible: true,
				},
				"email": {
					Value:   "nodisplayname@localhost.com",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: false},
				"last-name-error":  {Visible: false},
				"email-error":      {Visible: false},
				"profile-error":    {Visible: false},
			},
			email:     "nodisplayname@localhost.com",
			firstName: "Display",
			lastName:  "Nameless",
			testName:  "Successful profile load with no display name",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			userData := test.UserData{
				DisplayName: data.displayName,
				Email:       data.email,
				FirstName:   data.firstName,
				LastName:    data.lastName,
			}

			token, err := test.CreateSession(ctx, logger, db, userData, time.Minute*5, userAgent)
			if err != nil {
				t.Fatal("Could not create a test sesssion for ", data.testName, err)
			}
			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    token,
			}

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/profile", nil)
			if err != nil {
				t.Fatal("Error building profile request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", userAgent)
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error getting the profile page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Got an error status from the server!")
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			err = test.ValidatePage(doc, data.elements)
			if err != nil {
				t.Fatal(err)
			}

		})

	}

}

func TestProfileEndpointsBadTemplates(t *testing.T) {

	env := map[string]string{
		"STATIC_FILES_DIR": filepath.Join("..", "..", "cmd", "web"),
		"TEMPLATES_DIR":    "templates",
	}
	testGetenv := func(name string) string { return env[name] }

	appHandler, err := server.NewServer(testGetenv, db, logger, nil)
	if err != nil {
		log.Fatal("Error setting up the test handler", err)
	}

	testData := []struct {
		formData url.Values
		method   string
		path     string
		testName string
		userData test.UserData
	}{
		{
			formData: url.Values{},
			method:   "GET",
			path:     "/profile",
			testName: "Get Profile",
			userData: test.UserData{
				Email:     "getprofilebadtemplate@localhost.com",
				FirstName: "Get",
				LastName:  "Profile",
			},
		},
		{
			formData: url.Values{
				"displayName": []string{"Changeme"},
				"email":       []string{"updateprofilebadtemplate@localhost.com"},
				"firstName":   []string{"Update"},
				"lastName":    []string{"Profile"},
			},
			method:   "POST",
			path:     "/profile",
			testName: "Update Profile",
			userData: test.UserData{
				Email:     "updateprofilebadtemplate@localhost.com",
				FirstName: "Update",
				LastName:  "Profile",
			},
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			templatesServer := httptest.NewServer(appHandler)
			defer templatesServer.Close()

			token, err := test.CreateSession(
				ctx,
				logger,
				db,
				data.userData,
				time.Minute*5,
				userAgent,
			)
			if err != nil {
				t.Fatal("Could not create a test session!", err)
			}

			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    token,
			}

			req, err := http.NewRequestWithContext(
				ctx,
				data.method,
				templatesServer.URL+data.path,
				strings.NewReader(data.formData.Encode()),
			)
			if err != nil {
				t.Fatal("Error building profile update request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", userAgent)
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error getting the updated profile page!", err)
			} else if res.StatusCode != http.StatusInternalServerError {
				t.Fatal("Expected a 500 from the server")
			}

		})

	}

}

func TestProfileUpdates(t *testing.T) {

	testData := []struct {
		displayName     string
		elements        map[string]test.ElementValidation
		email           string
		firstName       string
		lastName        string
		testName        string
		updatedUserData test.UserData
		userData        test.UserData
	}{
		{
			elements: map[string]test.ElementValidation{
				"profile-form": {Visible: true},
				"profile-header": {
					Value:   "Completed Modification Profile Page",
					Visible: true,
				},
				"first-name": {
					Value:   "Completed",
					Visible: true,
				},
				"last-name": {
					Value:   "Modification",
					Visible: true,
				},
				"display-name": {
					Value:   "Sudo",
					Visible: true,
				},
				"email": {
					Value:   "completedupdate@localhost.com",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: false},
				"last-name-error":  {Visible: false},
				"email-error":      {Visible: false},
				"profile-error":    {Visible: false},
			},
			testName: "Successful profile update changed",
			updatedUserData: test.UserData{
				DisplayName: "Sudo",
				Email:       "completedupdate@localhost.com",
				FirstName:   "Completed",
				LastName:    "Modification",
			},
			userData: test.UserData{
				DisplayName: "Root",
				Email:       "successfulupdate@localhost.com",
				ExternalID:  "success_update",
				FirstName:   "Successful",
				LastName:    "Update",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form": {Visible: true},
				"profile-header": {
					Value:   " Name Profile Page",
					Visible: true,
				},
				"first-name": {
					Value:   "",
					Visible: true,
				},
				"last-name": {
					Value:   "Name",
					Visible: true,
				},
				"display-name": {
					Value:   "Sudo",
					Visible: true,
				},
				"email": {
					Value:   "failedupdatenofirstname@localhost.com",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: true},
				"last-name-error":  {Visible: false},
				"email-error":      {Visible: false},
				"profile-error":    {Visible: false},
			},
			testName: "Failed update no first name",
			updatedUserData: test.UserData{
				DisplayName: "Sudo",
				Email:       "failedupdatenofirstname@localhost.com",
				FirstName:   "",
				LastName:    "Name",
			},
			userData: test.UserData{
				DisplayName: "Root",
				Email:       "failedupdatenofirstname@localhost.com",
				ExternalID:  "bad_first_name",
				FirstName:   "Nofirst",
				LastName:    "Name",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form": {Visible: true},
				"profile-header": {
					Value:   "Completed  Profile Page",
					Visible: true,
				},
				"first-name": {
					Value:   "Completed",
					Visible: true,
				},
				"last-name": {
					Value:   "",
					Visible: true,
				},
				"display-name": {
					Value:   "Root",
					Visible: true,
				},
				"email": {
					Value:   "",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: false},
				"last-name-error":  {Visible: true},
				"email-error":      {Visible: true},
				"profile-error":    {Visible: false},
			},
			testName: "Failed profile update last name and email",
			updatedUserData: test.UserData{
				DisplayName: "Root",
				Email:       "",
				FirstName:   "Completed",
				LastName:    "",
			},
			userData: test.UserData{
				DisplayName: "Root",
				Email:       "failedupdatemultipleFields@localhost.com",
				ExternalID:  "bad_last_email",
				FirstName:   "Successful",
				LastName:    "Update",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form": {Visible: true},
				"profile-header": {
					Value:   "Clear Displayname Profile Page",
					Visible: true,
				},
				"first-name": {
					Value:   "Clear",
					Visible: true,
				},
				"last-name": {
					Value:   "Displayname",
					Visible: true,
				},
				"display-name": {
					Value:   "Clear",
					Visible: true,
				},
				"email": {
					Value:   "cleardisplayname@localhost.com",
					Visible: true,
				},
				"profile-submit":   {Visible: true},
				"first-name-error": {Visible: false},
				"last-name-error":  {Visible: false},
				"email-error":      {Visible: false},
				"profile-error":    {Visible: false},
			},
			testName: "Clear display name",
			updatedUserData: test.UserData{
				DisplayName: "",
				Email:       "cleardisplayname@localhost.com",
				FirstName:   "Clear",
				LastName:    "Displayname",
			},
			userData: test.UserData{
				DisplayName: "Blanked",
				Email:       "cleardisplayname@localhost.com",
				ExternalID:  "clear_display",
				FirstName:   "Clear",
				LastName:    "Displayname",
			},
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			token, err := test.CreateSession(ctx, logger, db, data.userData, time.Minute*5, userAgent)
			if err != nil {
				t.Fatal("Could not create a test sesssion for ", data.testName, err)
			}
			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    token,
			}

			form := url.Values{}
			form.Add("email", data.updatedUserData.Email)
			form.Add("firstName", data.updatedUserData.FirstName)
			form.Add("lastName", data.updatedUserData.LastName)
			form.Add("displayName", data.updatedUserData.DisplayName)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/profile", strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal("Error building profile update request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", userAgent)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("Error getting the updated profile page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Got an error status from the server!", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			err = test.ValidatePage(doc, data.elements)
			if err != nil {
				t.Fatal(err)
			}

		})

	}

}
