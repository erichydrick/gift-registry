package server_test

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// Connection details for the test database
const (
	dbName = "server_test"
	dbUser = "server_user"
	dbPass = "server_pass"
)

// Test-specific values
var (
	browsers []playwright.BrowserType
	ctx      context.Context
	logger   *slog.Logger
	pw       *playwright.Playwright
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	/* Install playwright dependencies */
	err := playwright.Install()
	if err != nil {
		log.Fatal("Error installing Playwright dependencies! ", err)
	}

	pw, err = playwright.Run()
	if err != nil {
		log.Fatal("Error running Playwright!")
	}

	browsers = []playwright.BrowserType{
		pw.Chromium,
		pw.Firefox,
		pw.WebKit,
	}

	exitCode := m.Run()
	os.Exit(exitCode)

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
