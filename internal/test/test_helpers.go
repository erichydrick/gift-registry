package test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/net/html"
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

func (em *EmailMock) SendVerificationEmail(to []string, code string, getenv func(string) string) error {

	em.Token = code
	em.VerificationEmailSent = true
	return nil

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

func CheckElement(root html.Node, id string) (html.Node, bool) {

	/*
		If this element has the ID we're looking for, return true.
	*/
	if slices.Contains(root.Attr, html.Attribute{Key: "id", Val: id}) {
		return root, true
	}

	/*
		Do a depth-first search of all this element's children
		to see if any of them match the ID we're looking for.
	*/
	for node := range root.Descendants() {

		if _, ok := CheckElement(*node, id); ok {
			return *node, true
		}

	}

	return html.Node{}, false

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

func ElementVisible(node html.Node) bool {

	for _, attr := range node.Attr {

		/*
			An element is visible if it does not have the hidden property and does not
			have the "hidden" class. We don't care about any other attribute
		*/
		switch attr.Key {

		/* The hidden property means the element is not visible */
		case "hidden":
			return false
		case "class":
			/* The "hidden" class will set the element's display to none */
			if strings.Contains(attr.Val, "hidden") {
				return false
			}
		default:
			continue

		}

	}

	/* Assume the element is visible by default */
	return true

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
