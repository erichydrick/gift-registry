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
	dbUser                 = "profile_user"
	dbPass                 = "profile_pass"
	userAgent              = "test-user-agent"
	lookupUpdatedUserQuery = `
		SELECT p.person_id, 
			h.household_id,
			p.first_name, 
			p.last_name, 
			p.display_name, 
			p.email,
			h.name
		FROM person p 
			INNER JOIN household_person hp ON hp.person_id = p.person_id
			INNER JOIN household h ON h.household_id = hp.household_id
		WHERE p.external_id = $1`
)

// Test-specific values
var (
	ctx        context.Context
	db         database.Database
	dbPath     string
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
		userData    test.UserData
		managedData []test.UserData
		elements    map[string]test.ElementValidation
		testName    string
	}{
		{
			elements: map[string]test.ElementValidation{
				"profile-header-succ-disp-name": {
					Value:   "Root Named Profile",
					Visible: true,
				},
				"profile-form-succ-disp-name": {Visible: true},
				"first-name-succ-disp-name": {
					Value:   "Display",
					Visible: true,
				},
				"last-name-succ-disp-name": {
					Value:   "Named",
					Visible: true,
				},
				"display-name-succ-disp-name": {
					Value:   "Root",
					Visible: true,
				},
				"email-succ-disp-name": {
					Value:   "displayName@localhost.com",
					Visible: true,
				},
				"household-name-succ-disp-name": {
					Value:   "Disp",
					Visible: true,
				},
				"profile-submit-succ-disp-name":   {Visible: true},
				"first-name-error-succ-disp-name": {Visible: false},
				"household-error-succ-disp-name":  {Visible: false},
				"last-name-error-succ-disp-name":  {Visible: false},
				"email-error-succ-disp-name":      {Visible: false},
				"profile-error-succ-disp-name":    {Visible: false},
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "displayName@localhost.com",
				ExternalID:      "succ-disp-name",
				FirstName:       "Display",
				HouseholdName:   "Disp",
				LastName:        "Named",
			},
			testName: "Successful profile load with display name",
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-succ-def-disp-name": {Visible: true},
				"profile-header-succ-def-disp-name": {
					Value:   "Display Nameless Profile",
					Visible: true,
				},
				"first-name-succ-def-disp-name": {
					Value:   "Display",
					Visible: true,
				},
				"last-name-succ-def-disp-name": {
					Value:   "Nameless",
					Visible: true,
				},
				"display-name-succ-def-disp-name": {
					Value:   "Display",
					Visible: true,
				},
				"email-succ-def-disp-name": {
					Value:   "nodisplayname@localhost.com",
					Visible: true,
				},
				"household-name-succ-def-disp-name": {
					Value:   "Display",
					Visible: true,
				},
				"profile-submit-succ-def-disp-name":   {Visible: true},
				"first-name-error-succ-def-disp-name": {Visible: false},
				"last-name-error-succ-def-disp-name":  {Visible: false},
				"email-error-succ-def-disp-name":      {Visible: false},
				"profile-error-succ-def-disp-name":    {Visible: false},
			},
			userData: test.UserData{
				CreateHousehold: true,
				Email:           "nodisplayname@localhost.com",
				ExternalID:      "succ-def-disp-name",
				FirstName:       "Display",
				HouseholdName:   "Display",
				LastName:        "Nameless",
			},
			testName: "Successful profile load with no display name",
		},
		{
			elements: map[string]test.ElementValidation{
				// Main profile
				"profile-header-manager-profile": {
					Value:   "Root Named Profile",
					Visible: true,
				},
				"profile-form-manager-profile": {Visible: true},
				"first-name-manager-profile": {
					Value:   "Display",
					Visible: true,
				},
				"last-name-manager-profile": {
					Value:   "Named",
					Visible: true,
				},
				"display-name-manager-profile": {
					Value:   "Root",
					Visible: true,
				},
				"email-manager-profile": {
					Value:   "profilewithkids@localhost.com",
					Visible: true,
				},
				"household-name-manager-profile": {
					Value:   "With Kids",
					Visible: true,
				},
				"profile-submit-manager-profile":   {Visible: true},
				"first-name-error-manager-profile": {Visible: false},
				"household-error-manager-profile":  {Visible: false},
				"last-name-error-manager-profile":  {Visible: false},
				"email-error-manager-profile":      {Visible: false},
				"profile-error-manager-profile":    {Visible: false},
				// First child profile
				"profile-header-child-1-profile": {
					Value:   "Junior Named Profile",
					Visible: true,
				},
				"profile-form-child-1-profile": {Visible: true},
				"first-name-child-1-profile": {
					Value:   "Firstborn",
					Visible: true,
				},
				"last-name-child-1-profile": {
					Value:   "Named",
					Visible: true,
				},
				"display-name-child-1-profile": {
					Value:   "Junior",
					Visible: true,
				},
				"profile-submit-child-1-profile":   {Visible: true},
				"first-name-error-child-1-profile": {Visible: false},
				"last-name-error-child-1-profile":  {Visible: false},
				"profile-error-child-1-profile":    {Visible: false},
				// Second child profile
				"profile-header-child-2-profile": {
					Value:   "Baby Named Profile",
					Visible: true,
				},
				"profile-form-child-2-profile": {Visible: true},
				"first-name-child-2-profile": {
					Value:   "Secondborn",
					Visible: true,
				},
				"last-name-child-2-profile": {
					Value:   "Named",
					Visible: true,
				},
				"display-name-child-2-profile": {
					Value:   "Baby",
					Visible: true,
				},
				"profile-submit-child-2-profile":   {Visible: true},
				"first-name-error-child-2-profile": {Visible: false},
				"last-name-error-child-2-profile":  {Visible: false},
				"profile-error-child-2-profile":    {Visible: false},
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "profilewithkids@localhost.com",
				ExternalID:      "manager-profile",
				FirstName:       "Display",
				HouseholdName:   "With Kids",
				LastName:        "Named",
			},
			managedData: []test.UserData{
				{
					CreateHousehold: false,
					DisplayName:     "Junior",
					ExternalID:      "child-1-profile",
					FirstName:       "Firstborn",
					HouseholdName:   "With Kids",
					LastName:        "Named",
					Type:            "MANAGED",
				},
				{
					CreateHousehold: false,
					DisplayName:     "Baby",
					ExternalID:      "child-2-profile",
					FirstName:       "Secondborn",
					HouseholdName:   "With Kids",
					LastName:        "Named",
					Type:            "MANAGED",
				},
			},
			testName: "Profile load with associated managed profiles",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			token, err := test.CreateSession(ctx, logger, db, data.userData, time.Minute*5, userAgent)
			if err != nil {
				t.Fatal("Could not create a test sesssion for ", data.testName, err)
			}

			if len(data.managedData) > 0 {

				for _, managedProfile := range data.managedData {

					_, err := test.CreateUser(ctx, logger, db, managedProfile)
					if err != nil {
						t.Fatal("Could not create child profile", err)
					}

				}

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
				Email:      "getprofilebadtemplate@localhost.com",
				ExternalID: "profile-load-bad-temp",
				FirstName:  "Get",
				LastName:   "Profile",
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
			path:     "/profile/profile-update-bad-temp",
			testName: "Update Profile",
			userData: test.UserData{
				Email:      "updateprofilebadtemplate@localhost.com",
				ExternalID: "profile-update-bad-temp",
				FirstName:  "Update",
				LastName:   "Profile",
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
				t.Fatal("Expected a 500 from the server, but got", res.StatusCode)
			}

		})

	}

}

func TestProfileUpdates(t *testing.T) {

	testData := []struct {
		displayName     string
		elements        map[string]test.ElementValidation
		email           string
		externalID      string
		firstName       string
		householdName   string
		lastName        string
		managedData     []test.UserData
		success         bool
		testName        string
		updatedUserData test.UserData
		userData        test.UserData
	}{
		{
			elements: map[string]test.ElementValidation{
				"profile-form-success-update": {Visible: true},
				"profile-header-success-update": {
					Value:   "Sudo Modification Profile",
					Visible: true,
				},
				"first-name-success-update": {
					Value:   "Completed",
					Visible: true,
				},
				"last-name-success-update": {
					Value:   "Modification",
					Visible: true,
				},
				"display-name-success-update": {
					Value:   "Sudo",
					Visible: true,
				},
				"email-success-update": {
					Value:   "completedupdate@localhost.com",
					Visible: true,
				},
				"household-name-success-update": {
					Value:   "New House Success",
					Visible: true,
				},
				"profile-submit-success-update":   {Visible: true},
				"first-name-error-success-update": {Visible: false},
				"household-error-success-update":  {Visible: false},
				"last-name-error-success-update":  {Visible: false},
				"email-error-success-update":      {Visible: false},
				"profile-error-success-update":    {Visible: false},
			},
			success:  true,
			testName: "Successful profile update changed",
			updatedUserData: test.UserData{
				DisplayName:   "Sudo",
				Email:         "completedupdate@localhost.com",
				ExternalID:    "success-update",
				FirstName:     "Completed",
				HouseholdName: "New House Success",
				LastName:      "Modification",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "successfulupdate@localhost.com",
				ExternalID:      "success-update",
				FirstName:       "Successful",
				HouseholdName:   "Existing Household Success",
				LastName:        "Update",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-bad-first-name": {Visible: true},
				"profile-header-bad-first-name": {
					Value:   "Sudo Name Profile",
					Visible: true,
				},
				"first-name-bad-first-name": {
					Value:   "",
					Visible: true,
				},
				"last-name-bad-first-name": {
					Value:   "Name",
					Visible: true,
				},
				"display-name-bad-first-name": {
					Value:   "Sudo",
					Visible: true,
				},
				"email-bad-first-name": {
					Value:   "failedupdatenofirstname@localhost.com",
					Visible: true,
				},
				"household-name-bad-first-name": {
					Value:   "Failed update first name house",
					Visible: true,
				},
				"profile-submit-bad-first-name":   {Visible: true},
				"first-name-error-bad-first-name": {Visible: true},
				"household-error-bad-first-name":  {Visible: false},
				"last-name-error-bad-first-name":  {Visible: false},
				"email-error-bad-first-name":      {Visible: false},
				"profile-error-bad-first-name":    {Visible: false},
			},
			success:  false,
			testName: "Failed update no first name",
			updatedUserData: test.UserData{
				DisplayName:   "Sudo",
				Email:         "failedupdatenofirstname@localhost.com",
				ExternalID:    "bad-first-name",
				FirstName:     "",
				HouseholdName: "Failed update first name house",
				LastName:      "Name",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "failedupdatenofirstname@localhost.com",
				ExternalID:      "bad-first-name",
				FirstName:       "Nofirst",
				HouseholdName:   "Failed update first name house",
				LastName:        "Name",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-bad-last-email": {Visible: true},
				"profile-header-bad-last-email": {
					Value:   "Root  Profile",
					Visible: true,
				},
				"first-name-bad-last-email": {
					Value:   "FailedLastAndEmail",
					Visible: true,
				},
				"last-name-bad-last-email": {
					Value:   "",
					Visible: true,
				},
				"display-name-bad-last-email": {
					Value:   "Root",
					Visible: true,
				},
				"email-bad-last-email": {
					Value:   "",
					Visible: true,
				},
				"household-name-bad-last-email": {
					Value:   "Failed update last name and email house",
					Visible: true,
				},
				"profile-submit-bad-last-email":   {Visible: true},
				"first-name-error-bad-last-email": {Visible: false},
				"household-error-bad-last-email":  {Visible: false},
				"last-name-error-bad-last-email":  {Visible: true},
				"email-error-bad-last-email":      {Visible: true},
				"profile-error-bad-last-email":    {Visible: false},
			},
			success:  false,
			testName: "Failed profile update last name and email",
			updatedUserData: test.UserData{
				DisplayName:   "Root",
				Email:         "",
				ExternalID:    "bad-last-email",
				FirstName:     "FailedLastAndEmail",
				HouseholdName: "Failed update last name and email house",
				LastName:      "",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "failedupdatemultipleFields@localhost.com",
				ExternalID:      "bad-last-email",
				FirstName:       "FailedLastAndEmail",
				HouseholdName:   "Failed update last name and email house",
				LastName:        "Update",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-clear-display": {Visible: true},
				"profile-header-clear-display": {
					Value:   "Clear Displayname Profile",
					Visible: true,
				},
				"first-name-clear-display": {
					Value:   "Clear",
					Visible: true,
				},
				"last-name-clear-display": {
					Value:   "Displayname",
					Visible: true,
				},
				"display-name-clear-display": {
					Value:   "Clear",
					Visible: true,
				},
				"email-clear-display": {
					Value:   "cleardisplayname@localhost.com",
					Visible: true,
				},
				"household-name-clear-display": {
					Value:   "Clear display name success house",
					Visible: true,
				},
				"profile-submit-clear-display":   {Visible: true},
				"first-name-error-clear-display": {Visible: false},
				"household-error-clear-display":  {Visible: false},
				"last-name-error-clear-display":  {Visible: false},
				"email-error-clear-display":      {Visible: false},
				"profile-error-clear-display":    {Visible: false},
			},
			success:  true,
			testName: "Clear display name",
			updatedUserData: test.UserData{
				DisplayName:   "",
				Email:         "cleardisplayname@localhost.com",
				ExternalID:    "clear-display",
				FirstName:     "Clear",
				HouseholdName: "Clear display name success house",
				LastName:      "Displayname",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Blanked",
				Email:           "cleardisplayname@localhost.com",
				ExternalID:      "clear-display",
				FirstName:       "Clear",
				HouseholdName:   "Clear display name success house",
				LastName:        "Displayname",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-valid-household": {Visible: true},
				"profile-header-valid-household": {
					Value:   "Valid Household Profile",
					Visible: true,
				},
				"first-name-valid-household": {
					Value:   "Valid",
					Visible: true,
				},
				"last-name-valid-household": {
					Value:   "Household",
					Visible: true,
				},
				"display-name-valid-household": {
					Value:   "Valid",
					Visible: true,
				},
				"email-valid-household": {
					Value:   "validhouseholdname@localhost.com",
					Visible: true,
				},
				"household-name-valid-household": {
					Value:   "New valid household name",
					Visible: true,
				},
				"profile-submit-valid-household":   {Visible: true},
				"first-name-error-valid-household": {Visible: false},
				"household-error-valid-household":  {Visible: false},
				"last-name-error-valid-household":  {Visible: false},
				"email-error-valid-household":      {Visible: false},
				"profile-error-valid-household":    {Visible: false},
			},
			success:  false,
			testName: "Update household name",
			updatedUserData: test.UserData{
				DisplayName:   "Valid",
				Email:         "validhouseholdname@localhost.com",
				ExternalID:    "valid-household",
				FirstName:     "Valid",
				HouseholdName: "New valid household name",
				LastName:      "Household",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Valid",
				Email:           "validhouseholdname@localhost.com",
				ExternalID:      "valid-household",
				FirstName:       "Valid",
				HouseholdName:   "Valid household",
				LastName:        "Household",
			},
		},
		{
			elements: map[string]test.ElementValidation{
				"profile-form-update-managed-profile-2": {Visible: true},
				"profile-header-update-managed-profile-2": {
					Value:   "HasBeen Modified Profile",
					Visible: true,
				},
				"first-name-update-managed-profile-2": {
					Value:   "HasBeen",
					Visible: true,
				},
				"last-name-update-managed-profile-2": {
					Value:   "Modified",
					Visible: true,
				},
				"display-name-update-managed-profile-2": {
					Value:   "HasBeen",
					Visible: true,
				},
				"profile-submit-update-managed-profile-2":   {Visible: true},
				"first-name-error-update-managed-profile-2": {Visible: false},
				"last-name-error-update-managed-profile-2":  {Visible: false},
				"profile-error-update-managed-profile-2":    {Visible: false},
			},
			success:  true,
			testName: "Update managed profile",
			updatedUserData: test.UserData{
				DisplayName: "HasBeen",
				ExternalID:  "update-managed-profile-2",
				FirstName:   "HasBeen",
				LastName:    "Modified",
				Type:        "MANAGED",
			},
			userData: test.UserData{
				CreateHousehold: true,
				DisplayName:     "Root",
				Email:           "managedprofileupdate@localhost.com",
				ExternalID:      "update-manager-profile",
				FirstName:       "Successful",
				HouseholdName:   "Managed Household Success",
				LastName:        "Update",
				Type:            "NORMAL",
			},
			managedData: []test.UserData{
				{
					CreateHousehold: false,
					DisplayName:     "NotToBe",
					ExternalID:      "update-managed-profile-1",
					FirstName:       "NotToBe",
					HouseholdName:   "Managed Household Success",
					LastName:        "Modified",
					Type:            "MANAGED",
				},
				{
					CreateHousehold: false,
					DisplayName:     "NotYet",
					ExternalID:      "update-managed-profile-2",
					FirstName:       "HasBeen",
					HouseholdName:   "Managed Household Success",
					LastName:        "Modified",
					Type:            "MANAGED",
				},
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

			if len(data.managedData) > 0 {

				for _, managedProfile := range data.managedData {

					_, err := test.CreateUser(ctx, logger, db, managedProfile)
					if err != nil {
						t.Fatal("Could not create child profile", err)
					}

				}

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

			if data.success {

				var updatedRecord person
				db.QueryRow(ctx, lookupUpdatedUserQuery, data.updatedUserData.ExternalID).
					Scan(
						&updatedRecord.personID,
						&updatedRecord.householdID,
						&updatedRecord.firstName,
						&updatedRecord.lastName,
						&updatedRecord.displayName,
						&updatedRecord.email,
						&updatedRecord.householdName,
					)

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
