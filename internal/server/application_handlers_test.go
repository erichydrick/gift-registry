package server_test

import (
	"gift-registry/internal/test"
	"net/http"
	"testing"

	"golang.org/x/net/html"
)

func TestIndexHandler(t *testing.T) {

	testData := []struct {
		elements       map[string]bool
		expectedStatus int
		testName       string
	}{
		{
			elements: map[string]bool{
				"application-header": true,
				"page-content":       true,
				"redirector":         true,
			},
			expectedStatus: 200,
			testName:       "Success"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
			if err != nil {
				t.Fatal("error building landing page request", err)
			}

			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					res.Body.Close()
				}
			}()
			if err != nil {
				t.Fatal("server call failed", err)
			}

			if res.StatusCode != data.expectedStatus {

				t.Fatal("Expected a status code of ", data.expectedStatus, " but got ", res.StatusCode)

			}

			doc, err := html.Parse(res.Body)
			if err != nil {
				t.Fatal("error parsing the HTML content from the response", err)
			}

			for id, visible := range data.elements {

				if pageElem, ok := test.CheckElement(*doc, id); ok == false {

					t.Fatal("Could not find element", id, "on the page")

				} else if elemVis := test.ElementVisible(pageElem); elemVis != test.ElementVisible(pageElem) {

					t.Fatal("Expected element", id, "to have visibility =", visible, "but it was", elemVis)

				}
			}

		})

	}

}
