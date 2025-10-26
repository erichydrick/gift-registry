package test

import (
	"context"
	"crypto/rand"
	"fmt"
	"gift-registry/internal/database"
	"log"
	"log/slog"
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
	EmailToToken map[string]string
	EmailToSent  map[string]bool
}

const (
	DefaultUserAgent = "go-test-user-agent"
)

func (em *EmailMock) SendVerificationEmail(ctx context.Context, to []string, code string, getenv func(string) string) error {

	if em.EmailToSent == nil || em.EmailToToken == nil {
		log.Println("MAPS ARE NIL, WHO KNEW")
	}

	for _, email := range to {

		em.EmailToToken[email] = code
		em.EmailToSent[email] = true

	}

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
		testcontainers.WithWaitStrategyAndDeadline(20*time.Second, wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
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

func CreateSession(ctx context.Context, logger *slog.Logger, db database.Database, email string, timeLeft time.Duration, userAgent string) (string, error) {

	personID, err := CreateUser(ctx, logger, db, email)
	if err != nil {
		log.Println("Could not create user for", email)
		return "", err
	}

	token := rand.Text()

	/*
		Write the session record and sanity check that it's there.
	*/
	if res, err := db.Execute(ctx, "INSERT INTO session(session_id, person_id, expiration, user_agent) VALUES ($1, $2, $3, $4)", token, personID, time.Now().UTC().Add(timeLeft), userAgent); err != nil {
		return "", err
	} else if modified, err := res.RowsAffected(); err != nil {
		return "", err
	} else if modified != 1 {
		return "", fmt.Errorf("didn't have the expected number of database rows modified")
	}

	return token, nil

}

func CreateUser(ctx context.Context, logger *slog.Logger, db database.Database, email string) (int64, error) {

	id := int64(0)

	/*
		Do the insertion and make sure it worked. We're going to t.Fatal() if this
		fails, so I'm not going to worry about Rollback() calls erroring, the
		database is going to be deleted anyhow
	*/
	if res, err := db.Execute(ctx, "INSERT INTO person (email) VALUES ($1)", email); err != nil {
		log.Println("Error adding a new test person to the database.")
		return 0, err
	} else if added, err := res.RowsAffected(); err != nil {
		log.Println("Error getting the last inserted ID from the test person creation.")
		return 0, err
	} else if added < 1 {
		log.Println("Don't have an ID value for the newly-created person!")
		return 0, err
	}

	err := db.QueryRow(ctx, "SELECT person_id FROM person WHERE email = $1", email).Scan(&id)
	if err != nil {
		log.Println("Error reading the created user's ID")
		return 0, fmt.Errorf("error reading the created user's id: %v", err)
	}

	return id, nil

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
