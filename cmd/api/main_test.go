/*
This *should* be a separate package (main_test) to avoid direct access to
private methods, but I want to test gracefulShutdown without exporting it.
*/
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

var (
	ctx    context.Context
	logger *slog.Logger
)

// TestMain sets up the application tests by initializing a logger object to
// use in the methods and initializing a context.
func TestMain(m *testing.M) {

	/* Sets up a testing logger */
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, options)
	logger = slog.New(handler)

	ctx = context.Background()

	m.Run()

}

// TestShutdown validates the shutdown handler by starting an application server
// and triggering a shutdown signal to shut the server down
func TestShutdown(t *testing.T) {

	ctx := context.Background()

	testData := []struct {
		testName string
	}{
		{testName: "Graceful shutdown"},
	}

	for _, data := range testData {

		t.Run(data.testName, func(t *testing.T) {

			t.Parallel()

			done := make(chan bool, 1)
			server := &http.Server{}

			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			gracefulShutdown(ctx, server, done, func(context.Context) error { return nil }, logger)

			completed := <-done

			if !completed {

				t.Fatal("Expected the shutdown to have completed gracefully!")

			}

		})

	}
}
