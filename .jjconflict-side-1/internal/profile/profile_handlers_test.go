package profile_test

import (
	"context"
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

	"gift-registry/internal/database"
	"gift-registry/internal/middleware"
	"gift-registry/internal/server"
	"gift-registry/internal/test"

	"golang.org/x/net/html"
)

type person struct {
	personID      int64
	householdID   int64
	firstName     string
	lastName      string
	displayName   string
	email         string
	householdName string
}

// Connection details for the test database
const (
	dbName                 = "profile_test"
	lookupUpdatedUserQuery = `
		SELECT p.person_id, 
			h.household_id,
			p.first_name, 
			p.last_name, 
			p.display_name, 
			p.email,
			h.name
		FROM people p 
			INNER JOIN household_people hp ON hp.person_id = p.person_id
			INNER JOIN households h ON h.household_id = hp.household_id
		WHERE p.external_id = $1`
)

// Test-specific values
var (
	ctx              context.Context
	db               database.Database
	elementsFilePath string
	getenv           func(string) string
	logger           *slog.Logger
	testServer       *httptest.Server
)

func TestMain(m *testing.M) {
	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	var err error
	elementsFilePath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "profile_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not find the directory holding expected test output files.")
	}

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	env := map[string]string{
		"DATA_MIGRATIONS":  filepath.Join("..", "..", "testing_data", "profile_data", "sql"),
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

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)
}

func TestProfilePage(t *testing.T) {

	testData := []struct {
		elementsFile string
		testName     string
		token        string
	}{
		{
			elementsFile: "success_profile_with_display_name.json",
			token:        "succ-disp-name-token",
			testName:     "Successful profile load with display name",
		},
		{
			elementsFile: "success_profile_no_display_name.json",
			token:        "succ-def-disp-name-token",
			testName:     "Successful profile load with no display name",
		},
		{
			elementsFile: "success_profile_with_managed_profiles.json",
			token:        "manager-profile-token",
			testName:     "Profile load with associated managed profiles",
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

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/profile", nil)
			if err != nil {
				t.Fatal("Error building profile request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)
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
				t.Fatal("Error getting the profile page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Got an error status from the server! (", res.StatusCode, ")")
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			expectedElements, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected elements", err)
			}

			err = test.ValidatePage(doc, expectedElements)
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
		token    string
		testName string
	}{
		{
			formData: url.Values{},
			method:   "GET",
			path:     "/profile",
			testName: "Get Profile",
			token:    "profile-load-bad-temp-token",
		},
		{
			formData: url.Values{
				"displayName": []string{"Changeme"},
				"email":       []string{"updateprofilebadtemplate@localhost.com"},
				"firstName":   []string{"Update"},
				"lastName":    []string{"Profile"},
			},
			method:   "POST",
			path:     "/profile/profile-update-bad-temp",
			testName: "Update Profile",
			token:    "profile-update-bad-temp-token",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			templatesServer := httptest.NewServer(appHandler)
			defer templatesServer.Close()

			sessCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    data.token,
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
			req.Header.Set("User-Agent", test.DefaultUserAgent)
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
				t.Fatal("Error getting the updated profile page!", err)
			} else if res.StatusCode != http.StatusInternalServerError {
				t.Fatal("Expected a 500 from the server, but got", res.StatusCode)
			}
		})
	}
}

func TestProfileUpdates(t *testing.T) {

	testData := []struct {
		elementsFile    string
		success         bool
		testName        string
		token           string
		updatedUserData test.UserData
	}{
		{
			elementsFile: "success_profile_update.json",
			success:      true,
			testName:     "Successful profile update changed",
			token:        "success-update-token",
			updatedUserData: test.UserData{
				DisplayName:   "Sudo",
				Email:         "completedupdate@localhost.com",
				ExternalID:    "success-update",
				FirstName:     "Completed",
				HouseholdName: "New House Success",
				LastName:      "Modification",
			},
		},
		{
			elementsFile: "failed_profile_update_bad_first_name.json",
			success:      false,
			token:        "bad-first-name-token",
			testName:     "Failed update no first name",
			updatedUserData: test.UserData{
				DisplayName:   "Sudo",
				Email:         "failedupdatenofirstname@localhost.com",
				ExternalID:    "bad-first-name",
				FirstName:     "",
				HouseholdName: "Failed update first name house",
				LastName:      "Name",
			},
		},
		{
			elementsFile: "failed_profile_update_bad_last_name_and_email.json",
			success:      false,
			testName:     "Failed profile update last name and email",
			token:        "bad-last-email-token",
			updatedUserData: test.UserData{
				DisplayName:   "Root",
				Email:         "",
				ExternalID:    "bad-last-email",
				FirstName:     "FailedLastAndEmail",
				HouseholdName: "Failed update last name and email house",
				LastName:      "",
			},
		},
		{
			elementsFile: "success_profile_update_clear_display_name.json",
			success:      true,
			testName:     "Clear display name",
			token:        "clear-display-token",
			updatedUserData: test.UserData{
				DisplayName:   "",
				Email:         "cleardisplayname@localhost.com",
				ExternalID:    "clear-display",
				FirstName:     "Clear",
				HouseholdName: "Clear display name success house",
				LastName:      "Displayname",
			},
		},
		{
			elementsFile: "success_profile_update_change_household_name.json",
			success:      false,
			testName:     "Update household name",
			token:        "valid-household-token",
			updatedUserData: test.UserData{
				DisplayName:   "Valid",
				Email:         "validhouseholdname@localhost.com",
				ExternalID:    "valid-household",
				FirstName:     "Valid",
				HouseholdName: "New valid household name",
				LastName:      "Household",
			},
		},
		{
			elementsFile: "success_profile_update_modify_managed_profile.json",
			success:      true,
			testName:     "Update managed profile",
			token:        "update-manager-profile-token",
			updatedUserData: test.UserData{
				DisplayName: "HasBeen",
				ExternalID:  "update-managed-profile-2",
				FirstName:   "HasBeen",
				LastName:    "Modified",
				Type:        "MANAGED",
			},
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

			form := url.Values{}
			form.Add("email", data.updatedUserData.Email)
			form.Add("externalID", data.updatedUserData.ExternalID)
			form.Add("firstName", data.updatedUserData.FirstName)
			form.Add("lastName", data.updatedUserData.LastName)
			form.Add("displayName", data.updatedUserData.DisplayName)
			form.Add("householdName", data.updatedUserData.HouseholdName)
			form.Add("type", data.updatedUserData.Type)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/profile/"+data.updatedUserData.ExternalID, strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal("Error building profile update request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)
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
				t.Fatal("Error getting the updated profile page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Got an error status from the server!", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			expectedElements, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected elements", err)
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal(err)
			}

			if data.success {

				var updatedRecord person
				err = db.QueryRow(ctx, lookupUpdatedUserQuery, data.updatedUserData.ExternalID).
					Scan(
						&updatedRecord.personID,
						&updatedRecord.householdID,
						&updatedRecord.firstName,
						&updatedRecord.lastName,
						&updatedRecord.displayName,
						&updatedRecord.email,
						&updatedRecord.householdName,
					)
				if err != nil {
					t.Fatal("Error reading the updated row back out", err)
				}

				/* Confirm the database has the updated values */
				if updatedRecord.firstName != data.updatedUserData.FirstName {
					t.Fatal("Updated first name doesn't match the expected value! DB", updatedRecord.firstName, " expected", data.updatedUserData.FirstName)
				}
				if updatedRecord.lastName != data.updatedUserData.LastName {
					t.Fatal("Updated last name doesn't match the expected value!  DB", updatedRecord.lastName, " expected", data.updatedUserData.LastName)
				}
				if updatedRecord.displayName != data.updatedUserData.DisplayName {
					/*
						Clearing the display name causes the field to default to the first name
					*/
					if data.updatedUserData.DisplayName == "" && updatedRecord.displayName != updatedRecord.firstName {
						t.Fatal("Updated display name name doesn't match the expected value!DB", updatedRecord.displayName, " expected", data.updatedUserData.DisplayName)
					}
				}

				/* The following fields only get changed for non-managed profiles */
				if data.updatedUserData.Type != "MANAGED" && updatedRecord.email != data.updatedUserData.Email {
					t.Fatal("Updated email address doesn't match the expected value! DB", updatedRecord.email, " expected", data.updatedUserData.Email)
				}
				if data.updatedUserData.Type != "MANAGED" && updatedRecord.householdName != data.updatedUserData.HouseholdName {
					t.Fatal("Updated household doesn't match the expected value! DB", updatedRecord.householdName, " expected", data.updatedUserData.HouseholdName)
				}
			}
		})
	}
}
