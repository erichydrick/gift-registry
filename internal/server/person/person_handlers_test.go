package person_test

import (
	"context"
	"fmt"
	"gift-registry/internal/database"
	"gift-registry/internal/server"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"time"

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

// Test-specific values
var (
	ctx    context.Context
	logger *slog.Logger
)

func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	m.Run()

}

func TestSignups(t *testing.T) {

	/*
		TODO: NEED TO DETERMINE WHAT HAPPENS ON A SUCCESFUL SIGNUP
		PAGE SAYING WAITING ON A RESPONSE CODE
	*/

	/*
		TODO:
		SETUP A TEST SERVER WITH THE TEST DB
		POPULATE THE FORM AND SUBMIT IT
	*/

	ctx = context.Background()

	testData := []struct {
		pageError      bool
		errorMsgFields []string
		templatesDir   string
		testName       string
	}{
		/* TODO: ADD TEST DATA HERE */
	}

	env := map[string]string{
		"DB_USER":        dbUser,
		"DB_PASS":        dbPass,
		"DB_NAME":        dbName,
		"MIGRATIONS_DIR": filepath.Join("..", "..", "internal", "database", "migrations_test/success"),
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			port := freePort()
			dbCont, dbURL, err := buildDBContainer(ctx)
			if err != nil {
				t.Fatal("Error setting up a test database", err)
			}

			env["DB_HOST"] = strings.Split(dbURL, ":")[0]
			env["DB_PORT"] = strings.Split(dbURL, ":")[1]
			env["PORT"] = strconv.Itoa(port)
			env["TEMPLATES_DIR"] = data.templatesDir

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

			req, err := server.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
			if err != nil {
				t.Fatal("error building landing page request", err)
			}

			idxPage, err := http.DefaultClient.Do(req)
			defer func() {
				if idxPage != nil && idxPage.Body != nil {
					idxPage.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("server call failed", err)
			}

			/*
				TEST CASES:
				VALID SUBMISSION => NEW RECORD IN DB
				INVALID FIELDS => RETURN THE FORM, FIELDS STILL POPULATED, WITH ERROR MESSAGES VISIBLE
				INVALID FIELD => ONLY BAD FIELD HAS ERROR MESSAGE, ALL FIELDS HAVE ORIGINAL VALUE
				DB ERROR (INSERT A DUPLICATE) => RETURN FORM WITH DATA, AND PAGE-LEVEL ERROR MESSAGE
			*/
			logger.Debug("Test message", slog.Any("context", ctx))
			logger.Debug("Test message", slog.Any("DB Container", dbCont))
			logger.Debug("Test message", slog.Any("DBURL", dbURL))

		})

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
		postgres.WithInitScripts(filepath.Join("..", "..", "docker", "postgres_scripts", "init.sql")),
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
