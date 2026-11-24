package assert_test

import (
	"gift-registry/internal/assert"
	"os"
	"testing"
)

func TestMain(m *testing.M) {

	exitCode := m.Run()
	os.Exit(exitCode)

}

func TestAssert(t *testing.T) {

	testingData := []struct {
		cond     bool
		expected bool
		msg      string
		testName string
	}{
		{
			cond:     1 < 2,
			expected: false,
			msg:      "Assert not expected",
			testName: "Assertion passes",
		},
		{
			cond:     1 > 2,
			expected: true,
			msg:      "This should fail",
			testName: "Assertion fails",
		},
	}

	for _, data := range testingData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			defer func() {
				if rec := recover(); rec != nil {

					if !data.expected {

						t.Fatal("Should not have asserted")

					}

				} else if data.expected {

					t.Fatal("Should have assserted")

				}

			}()

			assert.Asssert(data.cond, data.msg)

		})

	}
}
