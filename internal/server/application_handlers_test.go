package server_test

import (
	"net/http"
	"testing"

	"gift-registry/internal/test"

	"golang.org/x/net/html"
)

func TestIndexHandler(t *testing.T) {
	testData := []struct {
		elementsFile   string
		expectedStatus int
		testName       string
	}{
		{
			elementsFile:   "index_page_elements.json",
			expectedStatus: 200,
			testName:       "Success",
		},
	}

	for _, data := range testData {
		t.Run(data.testName, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
			if err != nil {
				t.Fatal("error building landing page request", err)
			}

			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-Mode", "same-origin")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := http.DefaultClient.Do(req)
			defer func() {
				if res != nil && res.Body != nil {
					_ = res.Body.Close()
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

			elementsData, err := test.LoadExpectedElements(elementsFilePath, data.elementsFile)
			if err != nil {
				t.Fatal("Could not load expected output for validation!", err)
			}

			err = test.ValidatePage(doc, elementsData)
			if err != nil {
				t.Fatal("Could not validate expected output!", err)
			}

		})
	}
}
