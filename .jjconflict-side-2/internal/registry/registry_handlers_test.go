package registry_test

import (
	"context"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"gift-registry/internal/test"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	dbName    = "registry_test"
	userAgent = "test-user-agent"
	/*
		TODO: ADD QUERY CONSTANTS
	*/
)

var (
	ctx        context.Context
	db         database.Database
	getenv     func(string) string
	logger     *slog.Logger
	testServer *httptest.Server
)

func TestMain(m *testing.M) {

	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	srcDB, err := filepath.Abs(filepath.Join("..", "test", "test.db"))
	if err != nil {
		log.Fatal("Could not find test database source: ", err)
	}

	dbPath, err := filepath.Abs(filepath.Join(".", dbName))
	if err != nil {
		log.Fatal("Could not get path for test database ", err)
	}

	copied, err := test.SetupTestDatabase(srcDB, dbPath)
	if err != nil {
		log.Fatal("Could not create test database ", dbPath, ": ", err)
	}
	logger.InfoContext(
		ctx,
		"Created test database",
		slog.String("filename", dbPath),
		slog.Int64("size", copied),
	)

	env := map[string]string{
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

func TestRegistryPage(t *testing.T) {

	/* TODO: DEFINE THE TEST CASES */
	testData := []struct {
		elements map[string]test.ElementValidation
		itemData map[string][]test.ItemData
		userData []test.UserData /* TODO: HOW BAD DO I WANT MULTIPLE USERS/ITEMS? */
		testName string
	}{
		{
			elements: map[string]test.ElementValidation{
				"registries": test.ElementValidation{
					Visible: true,
				},
				"register-gift-btn": test.ElementValidation{
					Visible: true,
				},
			},
			itemData: map[string][]test.ItemData{
				"boy-child": {
					{
						AddedOn:    time.Now(),
						ExternalID: "new-bike",
						GiftDate:   time.Now().AddDate(0, 0, 7),
						Name:       "Bicycle",
						Notes:      "He's too big for the one he has now",
						Quantity:   1,
					},
					{
						AddedOn:    time.Now(),
						ExternalID: "harry-potter",
						GiftDate:   time.Now().AddDate(0, 0, 7),
						Name:       "Harry Potter collectibles",
						Quantity:   10,
						URL:        "https://hydrick.net/",
					},
				},
				"girl-child": {
					{
						AddedOn:    time.Now(),
						ExternalID: "mat",
						GiftDate:   time.Now().AddDate(0, 1, 0),
						Name:       "Tumbling mat",
						Quantity:   1,
						URL:        "https://hydrick.net",
					},
					{
						AddedOn:    time.Now(),
						ExternalID: "princess-dolls",
						GiftDate:   time.Now().AddDate(0, 1, 0),
						Name:       "Princess dolls",
						Notes:      "Barbie-style Disney princess dolls",
						Quantity:   1,
					},
				},
			},
			userData: []test.UserData{
				/* TODO: ADD PARENT, AND ADD GIRL-CHILD */
				{
					CreateHousehold: true,
					ExternalID:      "boy-child",
					FirstName:       "Boy",
					HouseholdName:   "Test House",
					LastName:        "Tester",
					Type:            "MANAGED",
				},
			},
		},
	}

	/*
		TODO: THINK ABOUT CREATING THE SESSION AND GETTING TEST USER IDS
	*/
	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			/* TODO: THIS IS WHERE THE MAGIC HAPPENS */

		})

	}

}
