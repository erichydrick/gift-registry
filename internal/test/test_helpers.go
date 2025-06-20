package test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Stub for the Emailer interface so I can validate emailing in automated
// testing
type EmailMock struct {
	Token                 string
	VerificationEmailSent bool
}

const (
	DefaultUserAgent = "go-test-user-agent"
	OutsideEmail     = "notarealuser@localhost.com"
	ValidEmail       = "hydrickgiftregistrytestuser@localhost.com"
)

var (
	browsers []playwright.BrowserType
)

func (em *EmailMock) SendVerificationEmail(to []string, code string, getenv func(string) string) error {

	em.Token = code
	em.VerificationEmailSent = true
	return nil

}

func BrowserList() ([]playwright.BrowserType, error) {

	if len(browsers) == 0 {

		pw, err := playwright.Run()
		if err != nil {
			log.Fatal("Error running Playwright!")
		}

		browsers = []playwright.BrowserType{
			pw.Chromium,
			pw.Firefox,
			pw.WebKit,
		}

	}

	return browsers, nil

}

func BuildDBContainer(ctx context.Context, initScripts string, dbName string, dbUser string, dbPass string) (*postgres.PostgresContainer, string, error) {

	dbCont, err := postgres.Run(
		ctx,
		"postgres:17.2",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPass),
		postgres.WithInitScripts(initScripts),
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

func CreateSession(ctx context.Context, db *sql.DB, timeLeft time.Duration, userAgent string) (string, error) {

	token := rand.Text()

	tx, err := db.BeginTx(ctx, nil)
	/*
		I'm not going to spend a lot of time handling errors here, just return them
		and fail the test.
	*/
	if err != nil {
		return "", err
	}

	/*
		Write the session record and sanity check that it's there.
	*/
	if res, err := db.ExecContext(ctx, "INSERT INTO session(session_id, email, expiration, user_agent) VALUES ($1, $2, $3, $4)", token, ValidEmail, time.Now().UTC().Add(timeLeft), userAgent); err != nil {
		return "", err
	} else if modified, err := res.RowsAffected(); err != nil {
		return "", err
	} else if modified != 1 {
		return "", fmt.Errorf("didn't have the expected number of database rows modified")
	}

	err = tx.Commit()
	if err != nil {
		return "", err
	}

	return token, nil

}

func CreateUser(ctx context.Context, db *sql.DB) error {

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Println("Error starting the create user transaction")
		return err
	}

	/*
		Do the insertion and make sure it worked. We're going to t.Fatal() if this
		fails, so I'm not going to worry about Rollback() calls erroring, the
		database is going to be deleted anyhow
	*/
	if res, err := db.ExecContext(ctx, "INSERT INTO person (email) VALUES ($1)", ValidEmail); err != nil {
		log.Println("Error adding a new test person to the database.")
		tx.Rollback()
		return err
	} else if added, err := res.RowsAffected(); err != nil {
		log.Println("Error getting the last inserted ID from the test person creation.")
		tx.Rollback()
		return err
	} else if added < 1 {
		log.Println("Don't have an ID value for the newly-created person!")
		tx.Rollback()
		return fmt.Errorf("did not complete insertion for test person")
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return err
	}

	return nil

}

// Asks the system for an open port I can use for a server or container Pulled from https://stackoverflow.com/a/43425461
func FreePort() (port int) {

	if listener, err := net.Listen("tcp", ":0"); err == nil {

		port = listener.Addr().(*net.TCPAddr).Port

	} else {

		log.Fatal("error getting open port", err)

	}

	return

}

// Returns a page launched in the given browser type that can then be
// populated for testing.
func GetPage(bType playwright.BrowserType) (playwright.Page, error) {

	browser, err := bType.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		log.Printf("Error launching browser! %v", err)
		return nil, fmt.Errorf("error creating the browser: %v", err)
	}

	browseContext, err := browser.NewContext()
	if err != nil {
		log.Printf("Error building browser context! %v", err)
		return nil, fmt.Errorf("error building the browser context: %v", err)
	}

	return browseContext.NewPage()

}

func ReadResult(res *http.Response) []byte {

	pgData := make([]byte, 256)
	readBytes := make([]byte, 256)
	for {

		numRead, err := res.Body.Read(readBytes)
		pgData = append(pgData, readBytes[:numRead]...)
		if err != nil {
			/* We finished reading the response body */
			if err == io.EOF {
				break
			} else {
				log.Fatal("Error reading page content!", err)
			}
		}

	}

	return pgData

}
