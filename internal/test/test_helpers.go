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

// Holds the details needed to validate page contents
type ElementValidation struct {
	Value   string
	Visible bool
}

// Stub for the Emailer interface so I can validate emailing in automated
// testing
type EmailMock struct {
	EmailToToken map[string]string
	EmailToSent  map[string]bool
}

// Holds the details needed to make a test user in the database
type UserData struct {
	DisplayName string
	Email       string
	ExternalID  string
	FirstName   string
	LastName    string
}

const (
	DefaultUserAgent = "go-test-user-agent"
	externalIDLength = 40
)

func (em *EmailMock) SendVerificationEmail(ctx context.Context, to []string, code string, getenv func(string) string) error {

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

		if childNode, ok := CheckElement(*node, id); ok {
			return childNode, true
		}

	}

	return html.Node{}, false

}

func CreateSession(ctx context.Context, logger *slog.Logger, db database.Database, userData UserData, timeLeft time.Duration, userAgent string) (string, error) {

	personID, err := CreateUser(ctx, logger, db, userData)
	if err != nil {
		log.Println("Could not create user for", userData, err)
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

func CreateUser(ctx context.Context, logger *slog.Logger, db database.Database, userData UserData) (int64, error) {

	id := int64(0)

	/*
		I don't want to have to make external IDs for every test, just use a string
		timestamp as a "good enough" placeholder
	*/
	if userData.ExternalID == "" {

		externalID := time.Now().String()
		userData.ExternalID = externalID[0:externalIDLength]

	}

	/*
		Do the insertion and make sure it worked. We're going to t.Fatal() if this
		fails, so I'm not going to worry about Rollback() calls erroring, the
		database is going to be deleted anyhow
	*/
	if res, err := db.Execute(ctx, "INSERT INTO person (external_id, email, first_name, last_name, display_name) VALUES ($1, $2, $3, $4, $5)", userData.ExternalID, userData.Email, userData.FirstName, userData.LastName, userData.DisplayName); err != nil {
		log.Println("Error adding a new test person to the database.")
		return 0, err
	} else if added, err := res.RowsAffected(); err != nil {
		log.Println("Error getting the last inserted ID from the test person creation.")
		return 0, err
	} else if added < 1 {
		log.Println("Don't have an ID value for the newly-created person!")
		return 0, err
	}

	err := db.QueryRow(ctx, "SELECT person_id FROM person WHERE email = $1", userData.Email).Scan(&id)
	if err != nil {
		log.Println("Error reading the created user's ID")
		return 0, fmt.Errorf("error reading the created user's id: %v", err)
	}

	return id, nil

}

// Checks if the element has the hidden property or hidden class.
// Returns true if either is found
func ElementVisible(node html.Node) bool {

	log.Printf("Checking attributes of %v\n", node)
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

// Goes through the mapping of elements to validation details and confirms that the given HTML has the expected elements with the given properties.
func ValidatePage(page *html.Node, elements map[string]ElementValidation) error {

	for id, validationInfo := range elements {

		if pageElem, ok := CheckElement(*page, id); !ok {

			return fmt.Errorf("could not find element %v on the page", id)

		} else if elemVis := ElementVisible(pageElem); elemVis != validationInfo.Visible {

			return fmt.Errorf("expected element %v to have visibility = %v, but it was %v", id, validationInfo.Visible, elemVis)

		} else if validationInfo.Value != "" {

			pageData := elementData(pageElem)
			if validationInfo.Value != pageData {

				return fmt.Errorf("expected element %v to have value = %v, but had %v",
					id, validationInfo.Value, pageData)

			}

		}

	}

	return nil

}

func elementData(pageElem html.Node) string {

	/*
		Prioritize the value attribute first. Then element body.
	*/
	for _, attr := range pageElem.Attr {

		/*
			Don't return on an empty value attribute value -
			try element body next.
		*/
		if attr.Key == "value" && attr.Val != "" {

			return attr.Val

		}

	}

	if pageElem.FirstChild != nil {

		return pageElem.FirstChild.Data

	}

	return ""

}
