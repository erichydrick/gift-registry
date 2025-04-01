package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"gift-registry/internal/database"
	"gift-registry/internal/server"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

/* Copied from the go-blueprint by Melkey for shutting down the server cleanly. */
func gracefulShutdown(apiServer *http.Server, done chan bool, logger *slog.Logger) {

	/* Create context that listens for the interrupt signal from the OS. */
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	/* Listen for the interrupt signal. */
	<-ctx.Done()

	logger.Info("Received the signal to shut down the server (press Ctrl+C again to force the server to quit immediately")

	/*
		The context is used to inform the server it has 5 seconds to finish
		the request it is currently handling
	*/
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown encountered an error, force quitting.", slog.String("errorMessage", err.Error()))
	}

	logger.Info("Server exiting")

	/* Notify the main goroutine that the shutdown is complete */
	done <- true
}

func initTracer(ctx context.Context, getenv func(string) string, logger *slog.Logger) (*sdktrace.TracerProvider, error) {

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	otelOTLPHTTPEndpoint := getenv("OTEL_OTLP_HTTP_ENDPOINT")

	if otelOTLPHTTPEndpoint == "" {

		return nil, errors.New("no otel otlp http endpoint defined")

	}

	otlptracehttp.NewClient()
	otlpHTTPExporter, err := otlptracehttp.New(
		ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint(otelOTLPHTTPEndpoint),
		otlptracehttp.WithURLPath("/api/default/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{"Authorization": getenv("OTEL_AUTHORIZATION")}),
	)
	if err != nil {
		return nil, err
	}

	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("family-gift-registry"),
		semconv.ServiceVersionKey.String("0.0.0"),
		attribute.String("environment", getenv("environment")),
	)

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource),
		sdktrace.WithBatcher(otlpHTTPExporter),
	)
	otel.SetTracerProvider(traceProvider)

	return traceProvider, nil

}

/* Launches and runs the application. Returns an error indicating a failure so the application can exit with a non-0 status */
func Run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {

	/* Create context that listens for the interrupt signal from the OS. */
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	/* Create a done channel to signal when the shutdown is complete */
	done := make(chan bool, 1)

	// TODO: FIGURE OUT WHAT TO DO WITH THIS TO EMIT TRACES FROM CALLING MY ENDPOINTS
	/* Set up telemetry exporting */
	traceProvider, err := initTracer(ctx, getenv, logger)
	if err != nil {
		logger.Error("error setting up opentelemetry integrations", slog.String("errorMessage", err.Error()))
		return err
	}
	defer func() {
		if err := traceProvider.Shutdown(ctx); err != nil {
			logger.Error("Error shutting down OTel trace provider", slog.String("errorMessage", err.Error()))
		}
	}()

	/* Get a database connection to pass to our handlers */
	db, err := database.Connection(getenv)
	if err != nil {
		logger.Error("Error connecting to the database", slog.String("errorMessage", err.Error()))
		return err
	}

	/* Set up the routing and middleware, we'll start the server in a sec */
	appHandler, err := server.NewServer(getenv, db, logger)
	if err != nil {
		return err
	}

	appServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", getenv("PORT")),
		Handler:      appHandler,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	/*
	   Run the graceful shutdown in a separate goroutine so it listens for
	   the shutdown signal in the background
	*/
	go gracefulShutdown(appServer, done, logger)

	/* Now we actually start and run the server */
	err = appServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		logger.Error("Error starting server", slog.String("errorMessage", err.Error()))
		return err
	}

	/* Wait for the graceful shutdown to complete */
	<-done
	logger.Info("Graceful shutdown complete.")
	<-ctx.Done()

	return nil

}

// Application entrypoint. Configures the logger, then runs, exiting with a non-0 status if startup fails.
func main() {

	/*
	   Configure logging
	*/
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewJSONHandler(os.Stderr, options)
	logger := slog.New(handler)

	ctx := context.Background()

	err := Run(ctx, os.Getenv, logger)
	if err != nil {
		logger.Error("error launching the application", slog.String("errorMessage", err.Error()))
		os.Exit(-1)
	}

}
