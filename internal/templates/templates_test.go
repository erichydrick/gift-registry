package templates_test

import (
	"bytes"
	"context"
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

type pageData struct {
	User string
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
		elementsFile  string
		fields        pageData
		templateFiles []string
		templateName  string
		testName      string
	}{
		{
			fields: pageData{
				User: "Non-Web",
			},
			templateFiles: []string{
				"test_non_webpage.html",
			},
			templateName: "test-non-browser",
			testName:     "Test non-HTML template",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			content := new(bytes.Buffer)
			_, err := fmt.Fprintf(content, "%s", data.fields)
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
				t.Fatal("Error writing to an output stream template", err)
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

			expectedElements, err := test.LoadExpectedElements(expectedElementsPath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not set test templates", err)
			}

			rootNode, err := html.Parse(strings.NewReader(string(msgValidation)))
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
			elementsFile: "test_page.json",
			pageData: pageData{
				User: "Tester",
			},
			pageName:      "test-page",
			templateFiles: []string{"test_page.html", "test_content.html"},
			templateName:  "test-page",
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
		templateFiles []string
		templateName  string
		testName      string
		values        pageData
	}{
		{
			elementsFile:  "test_template.json",
			errorExpected: false,
			templateFiles: []string{"test_content.html"},
			templateName:  "test-page-content",
			testName:      "Test profile form template",
			values: pageData{
				User: "Tester",
			},
		},
		{
			elementsFile:  "error_default.json",
			errorExpected: true,
			templateName:  "profile-thing-that-is-not-defined",
			templateFiles: []string{"test_content.html"},
			testName:      "Test default error response",
			values: pageData{
				User: "Tester",
			},
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
				data.values,
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
