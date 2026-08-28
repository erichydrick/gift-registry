package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Value   string `json:"value"`
	Visible bool   `json:"visible"`
}

// Stub for the Emailer interface so I can validate emailing in automated
// testing
type EmailMock struct {
	EmailToToken map[string]string
	EmailToSent  map[string]bool
}

// ItemData holds the details needed to make a test item in the database
type ItemData struct {
	AddedBy       int64
	AddedOn       time.Time
	ExternalID    string
	GiftDate      time.Time
	LastUpdatedOn time.Time
	Name          string
	Notes         string
	Quantity      int8
	PersonID      int64
	URL           string
}

// Holds the details needed to make a test user in the database
type UserData struct {
	CreateHousehold bool
	DisplayName     string
	Email           string
	ExternalID      string
	FirstName       string
	HouseholdName   string
	LastName        string
	PersonID        int64
	Type            string
}

const (
	DefaultUserAgent = "test-user-agent"
	externalIDLength = 40
)

func (em *EmailMock) SendVerificationEmail(
	ctx context.Context,
	to []string,
	code string,
	getenv func(string) string,
) error {

	for _, email := range to {

		em.EmailToToken[email] = code
		em.EmailToSent[email] = true

	}

	return nil
}

func BuildDBContainer(
	ctx context.Context,
	initScripts string,
	dbName string,
	dbUser string,
	dbPass string,
) (*postgres.PostgresContainer, string, error) {

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

// CleanupDatabase removes the libsql files associated with the database in
// the given (absolute path) file reference.
func CleanupDatabase(targetDB string) error {
	files, err := filepath.Glob(targetDB + "*")
	if err != nil {
		return fmt.Errorf("could not clean up test database %s: %v", targetDB, err)
	}

	for _, filename := range files {
		if err := os.Remove(filename); err != nil {
			return fmt.Errorf("could not clean up test file %s: %v", filename, err)
		}
	}

	return nil
}

// Checks if the element has the hidden property or hidden class.
// Returns true if either is found
func ElementVisible(node html.Node) bool {

	for _, attr := range node.Attr {

		/*
			An element is visible if it does not have the hidden property and does not
			have the "hidden" class. We don't care about any other attribute
		*/
		switch strings.ReplaceAll(attr.Key, " ", "") {

		/* The hidden property means the element is not visible */
		case "hidden":
			return false
		case "class":
			/* The "hidden" class will set the element's display to none */
			if strings.Contains(attr.Val, "hidden") {
				return false
			}
		case "type":
			/* Hidden inputs are not visible on the page */
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

func LoadExpectedElements(dirPath string, filename string) (map[string]ElementValidation, error) {
	elementData := map[string]ElementValidation{}
	dataFile, err := filepath.Abs(filepath.Join(dirPath, filename))
	if err != nil {
		return elementData, fmt.Errorf("could not get the full path for the expected elements file: %v", err)
	}

	jsonFile, err := os.Open(dataFile)
	if err != nil {
		return elementData, fmt.Errorf("could not open the expected elements file: %v", err)
	}
	defer func() {
		_ = jsonFile.Close()
	}()

	jsonBytes, err := io.ReadAll(jsonFile)
	if err != nil {
		return elementData, nil
	}

	err = json.Unmarshal(jsonBytes, &elementData)
	if err != nil {
		return map[string]ElementValidation{}, fmt.Errorf("could not convert json to element verification map: %v", err)
	}

	return elementData, nil
}

// ValidatePage goes through the mapping of elements to validation details and
// confirms that the given HTML has the expected elements with the given
// properties.
/* TODO: SHOULD I INCLUDE A NOT ON PAGE CHECK? */
func ValidatePage(page *html.Node, elements map[string]ElementValidation) error {

	for id, validationInfo := range elements {

		if pageElem, ok := CheckElement(*page, id); !ok {

			return fmt.Errorf("could not find element %v on the page", id)

		} else if validationInfo.Value != "" {

			pageData := elementData(pageElem)

			/*
				I like newlines and whitespace, and when it's in the HTML template
				I'm validating, the value check fails because it picks up the
				newline. I'm not doing anything where newlines (or their absence)
				would be relevant.
			*/
			pageData = strings.ReplaceAll(pageData, "\n", "")
			pageData = strings.Trim(pageData, " ")

			if validationInfo.Value != pageData {

				return fmt.Errorf("expected element %v to have value = %v, but had %v",
					id, validationInfo.Value, pageData)

			}

		} else if elemVis := ElementVisible(pageElem); elemVis != validationInfo.Visible {

			return fmt.Errorf("expected element %v to have visibility = %v, but it was %v", id, validationInfo.Visible, elemVis)

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
