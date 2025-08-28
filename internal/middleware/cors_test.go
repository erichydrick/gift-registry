package middleware_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Tests to confirm the CORS middleware is behaving as expected. Validates
// it automatically returns with the correct status code on an HTTP OPTIONS
// call, and returns with the appropriate headers in a valid endpoint call,
func TestCORS(t *testing.T) {

	testData := []struct {
		expectedStatusCode int
		methodName         string
		testName           string
	}{
		{
			expectedStatusCode: http.StatusOK,
			methodName:         "GET",
			testName:           "Regular call",
		},
		{
			expectedStatusCode: http.StatusNoContent,
			methodName:         "OPTIONS",
			testName:           "Options call",
		},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, data.methodName, testServer.URL+"/login", nil)
			if err != nil {
				t.Fatal("Error building a new request for the CORS test", err)
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal("Error calling the login page for testing", err)
			}

			if res.StatusCode != data.expectedStatusCode {

				t.Fatal("Expected to get a status code", data.expectedStatusCode, "but got", res.StatusCode, "instead.")

			}

			methodsHeader := res.Header.Get("Access-Control-Allow-Methods")
			methodList := strings.Split(methodsHeader, ",")

			if len(allowedMethods) != len(methodList) {
				t.Fatal("Expected a total of", len(allowedMethods), "but was", len(methodList))
			}
			for _, method := range methodList {

				method = strings.Trim(method, " ")
				if slices.Contains(allowedMethods, method) == false {

					t.Fatal(method, "allowed by CORS, but not in the expected list of allowed methods:", allowedMethods)

				}

			}

		})

	}
}
