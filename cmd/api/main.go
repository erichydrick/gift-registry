package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	"gift-registry/internal/database"
	"gift-registry/internal/server"
)

const (
	name = "net.hydrick.gift-registry"
)

/* Launches and runs the application. Returns an error indicating a failure so the application can exit with a non-0 status */
func Run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {

	/* Create context that listens for the interrupt signal from the OS. */
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	/* Create a done channel to signal when the shutdown is complete */
	done := make(chan bool, 1)

	/* Set up OpenTelemetry integration */
	otelShutdown, err := setupOTelSDK(ctx, getenv)
	if err != nil {
		logger.Error("Error setting up OpenTelemetry", slog.String("errorMessage", err.Error()))
		return err
	}
	defer func() {
		err = errors.Join(err, otelShutdown(ctx))
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
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
		Handler:      appHandler,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	/*
	   Run the graceful shutdown in a separate goroutine so it listens for
	   the shutdown signal in the background
	*/
	go gracefulShutdown(ctx, appServer, done, otelShutdown, logger)

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
	logger := otelslog.NewLogger(name, otelslog.WithSource(true))

	ctx := context.Background()

	err := Run(ctx, os.Getenv, logger)
	if err != nil {
		logger.Error("error launching the application", slog.String("errorMessage", err.Error()))
		os.Exit(-1)
	}

}

/* Copied from the go-blueprint by Melkey for shutting down the server cleanly. */
func gracefulShutdown(ctx context.Context, apiServer *http.Server, done chan bool, otelShutdown func(context.Context) error, logger *slog.Logger) {

	/* Create context that listens for the interrupt signal from the OS. */
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
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

	otelShutdown(ctx)
	logger.Info("Server exiting")

	/* Notify the main goroutine that the shutdown is complete */
	done <- true
}

/* TODO: FIGURE OUT HOW TO ENSURE MY SLOG.LOGGER IS THE LOGGER BEING CAPTURED */
/* Sets up the OTel logging provider */
func newLoggerProvider() (*log.LoggerProvider, error) {

	exporter, err := stdoutlog.New()
	if err != nil {
		return nil, err
	}

	logger := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(exporter)),
	)

	return logger, nil

}

/* Sets up the OTel meter provider */
func newMeterProvider() (*metric.MeterProvider, error) {

	exporter, err := stdoutmetric.New()
	if err != nil {
		return nil, err
	}

	meter := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exporter,
			metric.WithInterval( /*1 * time.Minute*/ 3*time.Second))),
	)

	return meter, nil

}

/* Sets up the OTel propagator */
func newPropagator() propagation.TextMapPropagator {

	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

}

/* Sets up the OTel tracing provider */
func newTracerProvier(ctx context.Context, getenv func(string) string) (*trace.TracerProvider, error) {

	otlptracehttp.NewClient()

	httpExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint(getenv("OTEL_OTLP_HTTP_ENDPOINT")),
		otlptracehttp.WithURLPath("/api/default/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{"Authorization": "Basic " + getenv("OTEL_AUTHORIZATION")}),
	)
	if err != nil {
		return nil, err
	}

	/*
		TODO: BOTH THE SERVICEVERSIONKEY AND THE ENVIRONMENT SHOULD BE SET VIA
		COMPOSE FILES PROPERTIES AND/OR ENVIRONMENT VARIABLES SO I DON'T HAVE
		TO REMEMBER TO MANUALLY EDIT THESE
	*/
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(name),
		semconv.ServiceVersionKey.String("0.0.1"),
		attribute.String("environment", "test"),
	)

	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithResource(res),
		trace.WithBatcher(httpExporter),
	)

	return tracerProvider, nil

}

/* Set up the OTel instrumentation and integration */
func setupOTelSDK(ctx context.Context, getenv func(string) string) (shutdown func(context.Context) error, err error) {

	var shutdownFuncs []func(context.Context) error

	/*
		Wrap all the registered OTel shutdown functions into 1 function call.
	*/
	shutdown = func(ctx context.Context) error {

		var err error
		for _, fn := range shutdownFuncs {

			err = errors.Join(err, fn(ctx))

		}

		shutdownFuncs = nil
		return err

	}

	/* Call shutdown should the Otel component setup ever return an error */
	errReturned := func(srcErr error) {

		err = errors.Join(srcErr, shutdown(ctx))

	}

	otel.SetTextMapPropagator(newPropagator())

	traceProvider, err := newTracerProvier(ctx, getenv)
	if err != nil {
		errReturned(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, traceProvider.Shutdown)
	otel.SetTracerProvider(traceProvider)

	metricProvider, err := newMeterProvider()
	if err != nil {
		errReturned(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, metricProvider.Shutdown)
	otel.SetMeterProvider(metricProvider)

	logProvider, err := newLoggerProvider()
	if err != nil {
		errReturned(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, logProvider.Shutdown)
	global.SetLoggerProvider(logProvider)

	return

}
