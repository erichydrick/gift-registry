package server_test

import (
	"context"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"log"
	"log/slog"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/testcontainers/testcontainers-go"
)

func TestSignupForm(t *testing.T) {

	ctx := context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	testData := []struct {
		expectedStatus int
	}{}

}

func TestSignups(t *testing.T) {

	/*
		TODO: NEED TO DETERMINE WHAT HAPPENS ON A SUCCESFUL SIGNUP
		PAGE SAYING WAITING ON A RESPONSE CODE
	*/

	ctx := context.Background()

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	testData := []struct {
		email                 string
		envOverrides          map[string]string
		expectedHiddenFields  []string
		expectedVisibleFields []string
		expectedStatusCode    int
		firstName             string
		lastName              string
		pageError             bool
		submitCount           int
		testName              string
	}{
		{email: "no", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error", "signup-first-name-error", "signup-last-name-error"}, expectedVisibleFields: []string{"signup-email-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "Test", lastName: "User", pageError: false, submitCount: 1, testName: "Bad Email"},
		{email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error", "signup-email-error", "signup-last-name-error"}, expectedVisibleFields: []string{"signup-first-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "", lastName: "User", pageError: false, submitCount: 1, testName: "Bad First Name"},
		{email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error", "signup-email-error", "signup-first-name-error"}, expectedVisibleFields: []string{"signup-last-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "Test", lastName: "", pageError: false, submitCount: 1, testName: "Bad Last Name"},
		{email: "no", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error"}, expectedVisibleFields: []string{"signup-email-error", "signup-first-name-error", "signup-last-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "", lastName: "", pageError: false, submitCount: 1, testName: "Bad Data"},
		{email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-email-error", "signup-first-name-error", "signup-last-name-error"}, expectedVisibleFields: []string{"signup-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "Test", lastName: "User", pageError: true, submitCount: 2, testName: "Duplicate registration"},
	}

	env := map[string]string{
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_NAME":        dbName,
		"MIGRATIONS_DIR": filepath.Join("..", "..", "..", "internal", "database", "migrations"),
		"TEMPLATES_DIR":  "../../../cmd/web/templates",
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			port := freePort()
			dbCont, dbURL, err := buildDBContainer(ctx)
			defer func() {
				if err := testcontainers.TerminateContainer(dbCont); err != nil {
					log.Fatal("Failed to terminate the database test container ", err)
				}
			}()
			if err != nil {
				t.Fatal("Error setting up a test database", err)
			}

			env["DB_HOST"] = strings.Split(dbURL, ":")[0]
			env["DB_PORT"] = strings.Split(dbURL, ":")[1]
			env["PORT"] = strconv.Itoa(port)

			/*
				Override environment values with test-specific ones if needed
			*/
			maps.Copy(env, data.envOverrides)
			getenv := func(name string) string { return env[name] }

			db, err := database.Connection(ctx, logger, getenv)
			if err != nil {
				t.Fatal("database connection failure! ", err)
			}

			appHandler, err := server.NewServer(getenv, db, logger)
			if err != nil {
				t.Fatal("error setting up the test handler", err)
			}

			testServer := httptest.NewServer(appHandler)
			defer testServer.Close()

			for _, bType := range browsers {

				log.Println("Testing against", bType.Name())

				page, err := getPage(bType)
				if err != nil {
					t.Fatalf("Error creating new webpage object %v", err)
				}

				/*
					Some tests will involve multiple submissions to validate an error scenario
				*/
				for range data.submitCount {

					resp, err := page.Goto(testServer.URL)
					if err != nil {
						t.Fatalf("Error opening the page %v", err)
					}

					_, err = resp.Body()
					if err != nil {
						t.Fatal("Error parsing response body! ", err)
					}

					locator := page.Locator("#signup-email")
					if locator == nil {
						t.Fatal("No email field!")
					}
					err = locator.Fill(data.email)
					if err != nil {
						t.Fatalf("Error populating the email: %v", err)
					}
					err = page.Locator("#signup-first-name").Fill(data.firstName)
					if err != nil {
						t.Fatalf("Error populating the first name: %v", err)
					}
					err = page.Locator("#signup-last-name").Fill(data.lastName)
					if err != nil {
						t.Fatalf("Error populating the last name: %v", err)
					}
					err = page.Locator("#signup-submit").Click()
					if err != nil {
						t.Fatalf("Error 'clicking' the submit button: %v", err)
					}

				}

				asserts := playwright.NewPlaywrightAssertions(500)
				anyText, err := regexp.Compile("...")
				if err != nil {
					t.Fatal("Error building regex to check for content")
				}

				/*
					Validate the expected fields are present, have data (we're not going to
					worry about the specific content in the automated test), and are visible
				*/
				for _, id := range data.expectedVisibleFields {

					locator := page.Locator("#" + id)
					locAsserts := asserts.Locator(locator)

					err := locAsserts.ToBeVisible()
					if err != nil {
						t.Fatalf("Expected element #%s to be visible", id)
					}

					err = locAsserts.ToContainText(anyText)
					if err != nil {
						t.Fatalf("Expected element #%s to have content", id)
					}

				}

				/*
					Validate the expected fields are have no data and are not visible
				*/
				for _, id := range data.expectedHiddenFields {

					locator := page.Locator("#" + id)
					locAsserts := asserts.Locator(locator)

					err := locAsserts.Not().ToBeVisible()
					if err != nil {
						t.Fatalf("Expected element #%s to be hidden", id)
					}

					err = locAsserts.Not().ToContainText(anyText)
					if err != nil {
						t.Fatalf("Expected element #%s to have no content", id)
					}

				}

				/*
					TEST CASES:
					VALID SUBMISSION => NEW RECORD IN DB
				*/
			}

		})

	}

}

func getPage(bType playwright.BrowserType) (playwright.Page, error) {

	browser, err := bType.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		log.Printf("ERROR LAUNCHING BROWSER! %v", err)
		return nil, fmt.Errorf("error creating the browser: %v", err)
	}

	browseContext, err := browser.NewContext()
	if err != nil {
		log.Printf("ERROR BUILDING BROWSER CONTEXT! %v", err)
		return nil, fmt.Errorf("error building the browser context: %v", err)
	}

	return browseContext.NewPage()

}
