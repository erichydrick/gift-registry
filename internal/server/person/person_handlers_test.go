package person_test

import (
	"context"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"log"
	"log/slog"
	"maps"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Connection details for the test database
const (
	dbName = "person_testing"
	dbUser = "person_user"
	dbPass = "iamaperson"
)

// Non-constant values
var (
	browsers []playwright.BrowserType
	ctx      context.Context
	logger   *slog.Logger
	pw       *playwright.Playwright
)

func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	/* Install playwright dependencies */
	err := playwright.Install()
	if err != nil {
		log.Fatal("Error installing Playwright dependencies! ", err)
	}

	pw, err = playwright.Run()
	if err != nil {
		log.Fatal("Error running Playwright!")
	}

	log.Println("Playwright running")
	browsers = []playwright.BrowserType{
		pw.Chromium,
		pw.Firefox,
		pw.WebKit,
	}

	log.Println("Running tests")
	exitCode := m.Run()
	log.Println("Finished with exit code ", exitCode)
	os.Exit(exitCode)

}

func TestSignups(t *testing.T) {

	/*
		TODO: NEED TO DETERMINE WHAT HAPPENS ON A SUCCESFUL SIGNUP
		PAGE SAYING WAITING ON A RESPONSE CODE
	*/

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
		// {email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error", "signup-email-error", "signup-last-name-error"}, expectedVisibleFields: []string{"signup-first-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "", lastName: "User", pageError: false, submitCount: 1, testName: "Bad First Name"},
		// {email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error", "signup-email-error", "signup-first-name-error"}, expectedVisibleFields: []string{"signup-last-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "Test", lastName: "", pageError: false, submitCount: 1, testName: "Bad Last Name"},
		// {email: "no", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-error"}, expectedVisibleFields: []string{"signup-email-error", "signup-first-name-error", "signup-last-name-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "", lastName: "", pageError: false, submitCount: 1, testName: "Bad Data"},
		// {email: "no@no.com", envOverrides: map[string]string{}, expectedHiddenFields: []string{"signup-email-error", "signup-first-name-error", "signup-last-name-error"}, expectedVisibleFields: []string{"signup-error", "signup-email", "signup-first-name", "signup-last-name"}, expectedStatusCode: 200, firstName: "Test", lastName: "User", pageError: true, submitCount: 2, testName: "Duplicate registration"},
	}
	log.Println("Built the test cases")

	env := map[string]string{
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_NAME":        dbName,
		"MIGRATIONS_DIR": filepath.Join("..", "..", "..", "internal", "database", "migrations"),
		"TEMPLATES_DIR":  "../../../cmd/web/templates",
	}
	log.Println("Built the environment")

	for _, bType := range browsers {

		for _, data := range testData {

			t.Run(data.testName, func(t *testing.T) {

				t.Parallel()

				log.Println("Getting port and DB info")
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
				log.Println("Environment setup complete")

				/*
					Override environment values with test-specific ones if needed
				*/
				maps.Copy(env, data.envOverrides)
				getenv := func(name string) string { return env[name] }
				log.Println("Environment overrides set")

				db, err := database.Connection(ctx, logger, getenv)
				if err != nil {
					t.Fatal("database connection failure! ", err)
				}
				log.Println("DB Connected!")

				appHandler, err := server.NewServer(getenv, db, logger)
				if err != nil {
					t.Fatal("error setting up the test handler", err)
				}
				log.Println("Routes initialized")

				testServer := httptest.NewServer(appHandler)
				defer testServer.Close()
				log.Println("Test server running!")

				page, err := getPage(bType)
				if err != nil {
					t.Fatalf("Error creating new webpage object %v", err)
				}
				log.Println("Got the page!")

				/*
					Some tests will involve multiple submissions to validate an error scenario
				*/
				for range data.submitCount {

					resp, err := page.Goto(testServer.URL)
					if err != nil {
						t.Fatalf("Error opening the page %v", err)
					}
					log.Println("Got-to the page!")

					body, err := resp.Body()
					if err != nil {
						t.Fatal("Error parsing response body! ", err)
					}
					log.Println("Response body: ", string(body))

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
				log.Println("Got the playwright assertions...")
				anyText, err := regexp.Compile("...")
				if err != nil {
					t.Fatal("Error building regex to check for content")
				}
				log.Println("Asssertions set up")

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

			})

		}

	}

}

/* TODO: CAN I EXPORT TEST UTLITIES? */
func buildDBContainer(ctx context.Context) (*postgres.PostgresContainer, string, error) {

	dbCont, err := postgres.Run(
		ctx,
		"postgres:17.2",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPass),
		postgres.WithInitScripts(filepath.Join("..", "..", "..", "docker", "postgres_scripts", "init.sql")),
		testcontainers.WithWaitStrategyAndDeadline(60*time.Second, wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to launch the database test container! %v", err)
	}

	dbURL, err := dbCont.Endpoint(ctx, "")
	if err != nil {
		return nil, "", fmt.Errorf("error getting the database endpoint %v", err)
	}

	return dbCont, dbURL, nil

}

/*
Asks the system for an open port I can use for a server or container
Pulled from https://stackoverflow.com/a/43425461
*/
func freePort() (port int) {

	if listener, err := net.Listen("tcp", ":0"); err == nil {

		port = listener.Addr().(*net.TCPAddr).Port

	} else {

		log.Fatal("error getting open port", err)

	}

	return

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
