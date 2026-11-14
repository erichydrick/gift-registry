package templates_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/templates"
	"gift-registry/internal/test"
	"io"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

/* TODO: REPLACE PAGES WITH TESTS VALUES AND TYPES WITH TEST PAGE-SPECIFIC TYPES */
type msgData struct {
	Code string
}

type profileErrors struct {
	Email        string
	ErrorMessage string
	FirstName    string
	Household    string
	LastName     string
}

type userData struct {
	DisplayName   string
	Errors        profileErrors
	Email         sql.NullString
	ExternalID    string
	FirstName     string
	HouseholdName string
	LastName      string
	Type          string
}

type pageData struct {
	DisplayName string
	LastName    string
	Profiles    []userData
}

var (
	ctx                  context.Context
	expectedElementsPath string
	logger               *slog.Logger
)

func TestMain(m *testing.M) {

	ctx = context.Background()

	options := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	var err error

	templates.Initialize(logger)

	expectedElementsPath, err = filepath.Abs(filepath.Join("..", "..", "testing_data", "templates_data", "expected_outputs"))
	if err != nil {
		log.Fatal("Could not load path to expected outputs directory", err)
	}

	exitCode := m.Run()

	os.Exit(exitCode)

}

func TestWriteBytes(t *testing.T) {

	testData := []struct {
		fields        msgData
		msg           string
		templateFiles []string
		templateName  string
		testName      string
	}{
		{
			fields: msgData{
				Code: "MySuperSecretCode",
			},
			msg: "Simulated email message",
			templateFiles: []string{
				"login_email.html",
			},
			templateName: "login-email",
			testName:     "Test non-HTML template",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			content := new(bytes.Buffer)
			_, err := fmt.Fprintf(content, "%s", data.msg)
			if err != nil {
				t.Fatal("Could not populate the test message buffer")
			}

			err = templates.WriteBytes(
				content,
				data.templateName,
				data.templateFiles,
				data.fields,
			)
			if err != nil {
				t.Fatal("Error writing to an output stream template")
			}

			msgValidation := make([]byte, 1024)
			for {

				_, err = content.Read(msgValidation)
				if err != nil && err == io.EOF {
					break
				} else if err != nil {
					t.Fatal("Error reading the template data back out", err)
				}

			}

			if !strings.Contains(string(msgValidation), data.fields.Code) {
				t.Fatal("fields not written into the template!")
			}

		})

	}

}

func TestWriteResponsePage(t *testing.T) {

	testData := []struct {
		elementsFile  string
		pageData      pageData
		pageName      string
		templateFiles []string
		templateName  string
		testName      string
	}{
		{
			elementsFile: "profile_page.json",
			pageData: pageData{
				DisplayName: "Demo",
				LastName:    "User",
				Profiles: []userData{
					{
						ExternalID:  "person-1",
						FirstName:   "Test",
						LastName:    "User",
						DisplayName: "Demo",
						Email: sql.NullString{
							String: "testuser@localhost.com",
							Valid:  true,
						},
						HouseholdName: "Test Household",
						Type:          "NORMAL",
					},
				},
			},
			pageName:      "profile-page",
			templateFiles: []string{"profile_page.html", "profile_form.html", "header.html"},
			templateName:  "profile-page",
			testName:      "Test profile page template",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			res := httptest.NewRecorder()
			err := templates.WriteResponsePage(
				ctx,
				res,
				200,
				data.templateName,
				data.templateFiles,
				data.pageData,
			)
			if err != nil {
				t.Fatal("Error writing the template based", err)
			}

			expectedElements, err := test.LoadExpectedElements(expectedElementsPath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not set test templates", err)
			}

			result := res.Result()
			defer func() {
				if result.Body != nil {
					result.Body.Close()
				}
			}()
			body, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatal("Error reading response", err)
			}

			rootNode, err := html.Parse(strings.NewReader(string(body)))
			if err != nil {
				t.Fatal("Could not parse response", err)
			}

			err = test.ValidatePage(rootNode, expectedElements)
			if err != nil {
				t.Fatal("Page validation failed", err)
			}

		})

	}

}

func TestWriteResponseTemplate(t *testing.T) {

	testData := []struct {
		elementsFile  string
		errorExpected bool
		pageData      userData
		pageName      string
		templateFiles []string
		templateName  string
		testName      string
	}{
		{
			elementsFile:  "profile_form.json",
			errorExpected: false,
			pageData: userData{
				ExternalID:  "person-1",
				FirstName:   "Test",
				LastName:    "User",
				DisplayName: "Demo",
				Email: sql.NullString{
					String: "testuser@localhost.com",
					Valid:  true,
				},
				HouseholdName: "Test Household",
				Type:          "NORMAL",
			},
			pageName:      "profile-form",
			templateFiles: []string{"profile_form.html"},
			templateName:  "profile-form",
			testName:      "Test profile form template",
		},
		{
			elementsFile:  "error_default.json",
			errorExpected: true,
			pageData: userData{
				ExternalID:  "person-1",
				FirstName:   "Test",
				LastName:    "User",
				DisplayName: "Demo",
				Email: sql.NullString{
					String: "testuser@localhost.com",
					Valid:  true,
				},
				HouseholdName: "Test Household",
				Type:          "NORMAL",
			},
			templateName:  "profile-thing-that-is-not-defined",
			templateFiles: []string{"profile_form.html"},
			testName:      "Test default error response",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			res := httptest.NewRecorder()
			err := templates.WriteResponseTemplate(
				ctx,
				res,
				200,
				data.templateName,
				data.templateFiles,
				data.pageData,
			)
			if err != nil && !data.errorExpected {
				t.Fatal("Error writing the template base", err)
			}

			expectedElements, err := test.LoadExpectedElements(expectedElementsPath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not set test templates", err)
			}

			result := res.Result()
			defer func() {
				if result.Body != nil {
					result.Body.Close()
				}
			}()
			body, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatal("Error reading response", err)
			}

			rootNode, err := html.Parse(strings.NewReader(string(body)))
			if err != nil {
				t.Fatal("Could not parse response", err)
			}

			err = test.ValidatePage(rootNode, expectedElements)
			if err != nil {
				t.Fatal("Page validation failed", err)
			}

		})

	}

}
