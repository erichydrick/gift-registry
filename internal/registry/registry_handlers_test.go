package registry_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

type itemValidation struct {
	giftFor   int64
	itemName  string
	itemQty   int
	itemURL   sql.NullString
	itemNotes sql.NullString
}

const (
	dbName          = "registry_test"
	userAgent       = "test-user-agent"
	selectItemQuery = `
		SELECT item.name,
			item.gift_for,
			item.quantity,
			item.url,
			item.notes
		FROM items item 
			INNER JOIN people person ON item.gift_for = person.person_id
		WHERE person.external_id = ?
			AND item.name = ?
	`
)

var (
	ctx                  context.Context
	db                   database.Database
	expectedElementsPath string
	getenv               func(string) string
	logger               *slog.Logger
	testServer           *httptest.Server
)

func TestMain(m *testing.M) {

	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	env := map[string]string{
		"DATA_MIGRATIONS":  filepath.Join("..", "..", "testing_data", "registry_data", "sql"),
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

	expectedElementsPath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "registry_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not load the path to the expected outputs directory", err)
	}

	exitCode := m.Run()

	err = test.CleanupDatabase(dbPath)
	if err != nil {
		log.Fatal("Error cleaning up the test ", err)
	}

	os.Exit(exitCode)

}

// TestRegistryPage confirms the registry page displays the correct elements
// for both the currenlty logged in user (who should not see claim information
// for their future gifts), and other users (who should see everything). It
// also validates viewing historical gifts.
func TestRegistryPage(t *testing.T) {

	testData := []struct {
		elementsFile   string
		historicalFlag string
		testName       string
		token          string
	}{
		{
			elementsFile: "success_registry_display_page.json",
			testName:     "Successful registry view",
			token:        "mom-registry-session",
		},
		{
			elementsFile: "success_registry_display_page_other_person.json",
			testName:     "Can view claim details for other households",
			token:        "grandma-registry-session",
		},
		{
			elementsFile: "success_registry_display_page_other_person_same_household.json",
			testName:     "Can see claimants for other household member's gifts",
			token:        "dad-registry-session",
		},
		{
			elementsFile:   "historical_success_registry_display_page.json",
			historicalFlag: "?historical=true",
			testName:       "Can see historical gifts when historical=true query param is added (self)",
			token:          "dad-registry-session",
		},
		{
			elementsFile:   "historical_success_registry_display_page.json",
			historicalFlag: "?historical=TRUE",
			testName:       "Can see historical gifts when historical=true query param is added (case-insensitive)",
			token:          "dad-registry-session",
		},
		{
			elementsFile:   "success_registry_display_page.json",
			historicalFlag: "?historical=false",
			testName:       "Shows the regular registry when historical is 'false'",
			token:          "mom-registry-session",
		},
		{
			elementsFile:   "success_registry_display_page.json",
			historicalFlag: "?historical=true,other",
			testName:       "Successful (future gift) registry view when historical is not a boolean string",
			token:          "mom-registry-session",
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

			path := fmt.Sprintf("/registry%s", data.historicalFlag)
			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+path, nil)
			if err != nil {
				t.Fatal("Error building registry request", err)
			}

			req.AddCookie(&sessCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")

			res, err := http.DefaultClient.Do(req)
			defer func() {
				_ = res.Body.Close()
			}()
			if err != nil {
				t.Fatal("Error getting the registry page!", err)
			} else if res.StatusCode != http.StatusOK {
				t.Fatal("Expected a success status from the server, but got", res.StatusCode)
			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing response body!", err)
			}

			expectedElements, err := test.LoadExpectedElements(expectedElementsPath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load the list of elements to validate!", err)
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal("Output validation failed!", err)
			}

		})

	}

}

// TestAddItem validates the behavior around adding an item to a wish list.
func TestAddItem(t *testing.T) {

	testData := []struct {
		elementsFile string
		itemLink     string
		itemName     string
		itemNotes    string
		itemQuantity int
		personID     string
		statusCode   int
		testName     string
		token        string
	}{
		{
			elementsFile: "add_item_registry_handling_start_empty.json",
			itemLink:     "https://www.amazon.com",
			itemName:     "Imaginary gift",
			itemNotes:    "I wish I could get this",
			itemQuantity: 1,
			personID:     "person-nine",
			statusCode:   200,
			testName:     "Add item to empty list",
			token:        "tiny-tim-session",
		},
		{
			elementsFile: "add_item_registry_handling_existing_items.json",
			itemLink:     "https://www.walmart.com",
			itemName:     "Additional gift",
			itemNotes:    "I would want this",
			itemQuantity: 3,
			personID:     "person-three",
			statusCode:   200,
			testName:     "Add item to existing list",
			token:        "mom-registry-session",
		},
		{
			elementsFile: "invalid_new_item.json",
			itemName:     "Toilet seat",
			itemQuantity: 0,
			personID:     "person-ten",
			statusCode:   400,
			testName:     "Invalid new request",
			token:        "cant-add-session",
		},
		{
			elementsFile: "invalid_additional_item.json",
			itemName:     "Sword",
			itemNotes:    "Every conquerer needs a weapon",
			itemQuantity: 0,
			personID:     "person-four",
			statusCode:   400,
			testName:     "Invalid additional request",
			token:        "dad-registry-session",
		},
		{
			elementsFile: "invalid_person_new_item.json",
			itemName:     "Toilet seat",
			itemQuantity: 1,
			personID:     "person-ten-thousand",
			statusCode:   500,
			testName:     "Invalid person",
			token:        "cant-add-session",
		},
		/*
			TODO:
			TEST CASES NEEDED (TO VERIFY SHOWING ERROR MESSAGES AND KEEP FORM VALUES):
			2. INVALID ITEM ALREADY IN DB (NOT SURE I CAN DO THAT...)
				-> SHOULD BE PART OF THE VIEW TESTS, BUT WHAT DO I WANT THIS TO ACTUALLY *DO*?
		*/
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()

			sessionCookie := http.Cookie{
				HttpOnly: true,
				MaxAge:   time.Now().UTC().Add(time.Minute * 1).Second(),
				Name:     middleware.SessionCookie,
				SameSite: http.SameSiteStrictMode,
				Secure:   true,
				Value:    data.token,
			}

			form := url.Values{}
			form.Add("itemName", data.itemName)
			form.Add("itemQty", strconv.Itoa(data.itemQuantity))
			form.Add("itemURL", data.itemLink)
			form.Add("itemNotes", data.itemNotes)

			req, err := http.NewRequestWithContext(ctx, "POST", testServer.URL+"/registry/"+data.personID, strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal("Error creating test request")
			}

			req.AddCookie(&sessionCookie)
			req.Header.Set("User-Agent", test.DefaultUserAgent)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal("Error adding an item to it's new registry")
			}
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
				}
			}()

			if res.StatusCode != data.statusCode {

				t.Fatal("Expected the add item call to have a status of ",
					data.statusCode, "but it returned", res.StatusCode)

			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("Error parsing server response!", err.Error())
			}
			expectedElements, err := test.LoadExpectedElements(expectedElementsPath,
				data.elementsFile)

			if err != nil {
				t.Fatal("Error trying to load expected response elements", err.Error())
			}

			err = test.ValidatePage(doc, expectedElements)
			if err != nil {
				t.Fatal("Error validating page", err.Error())
			}

			var savedItem itemValidation
			err = db.QueryRow(
				ctx,
				selectItemQuery,
				data.personID,
				data.itemName,
			).Scan(
				&savedItem.itemName,
				&savedItem.giftFor,
				&savedItem.itemQty,
				&savedItem.itemURL,
				&savedItem.itemNotes,
			)

			/*
				Some of these tests intentionally fail (and thus don't write to the DB).
				Don't let no rows automatically fail the test
			*/
			if err != nil && err != sql.ErrNoRows {
				t.Fatal("Error reading the saved item from the database.", err.Error())
			}

			/*
				Read the record from the database (if present), and validate what's
				in the database with what's expected.
			*/
			if data.statusCode > 300 {

				err = savedItem.validateItemNotSaved()
				if err != nil {
					t.Fatal("Expected to not have a record in the database, but 1 found anyways", err)
				}

			} else {

				err = savedItem.validateItemSaved(
					data.itemName,
					data.itemQuantity,
					data.itemNotes,
					data.itemLink,
				)
				if err != nil {
					t.Fatal("Database record mismatch with expected!", err)
				}

			}

		})

	}

}

func (newItem itemValidation) validateItemNotSaved() (err error) {

	nullDBValue := sql.NullString{
		Valid:  false,
		String: "",
	}

	if newItem.itemName != "" ||
		newItem.itemQty != 0 ||
		newItem.itemNotes != nullDBValue ||
		newItem.itemURL != nullDBValue {

		err = fmt.Errorf("had data in the database despite the fact the item shouldn't have been saved")

	}

	return err

}

func (newItem itemValidation) validateItemSaved(
	expectedName string,
	expectedQty int,
	expectedNotes string,
	expectedURL string,
) error {

	var errList []error

	if newItem.itemName != expectedName {

		errList = append(
			errList,
			fmt.Errorf("name didn't match, expected %v, but got %v", expectedName, newItem.itemName))

	}

	if newItem.itemQty != expectedQty {

		errList = append(
			errList,
			fmt.Errorf("quantities didn't match, expected %v, but got %v", expectedQty, newItem.itemQty),
		)

	}

	if expectedNotes != newItem.itemNotes.String {

		errList = append(
			errList,
			fmt.Errorf("notes didn't match, expected %v, but got %v", expectedNotes, newItem.itemNotes.String),
		)

	}

	if expectedURL != newItem.itemURL.String {

		errList = append(
			errList,
			fmt.Errorf("urls didn't match, expected %v, but got %v", expectedURL, newItem.itemURL.String),
		)

	}

	return errors.Join(errList...)

}
