package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	name = "net.hydrick.gift-registry/server/health"
)

type healthStatus struct {
	DBHealth     healthInfo
	Healthy      bool
	ObservHealth healthInfo
}

type healthInfo struct {
	Error   string
	Healthy bool
}

var (
	meter          = otel.Meter(name)
	tracer         = otel.Tracer(name)
	healthCheckCtr metric.Int64Counter
)

func init() {

	var err error
	healthCheckCtr, err = meter.Int64Counter(
		"health.check.counter",
		metric.WithDescription("Number of calls to the /health endpoint"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		panic(err)
	}

}

// Checks the health of the application and returns some relevant statistics
func HealthCheckHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "health")
		defer span.End()

		dbStatus, err := dbHealth(ctx, svr.DB)
		svr.Logger.DebugContext(ctx, "DB status info obtained", slog.Any("statusObj", dbStatus))
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error getting database health data", slog.String("errorMessage", err.Error()))
			dbStatus.Error = err.Error()
		}

		observStatus, err := observHealth(svr.Getenv, svr.Logger)
		svr.Logger.DebugContext(ctx, "Observability health info obtained", slog.Any("statusObj", observStatus))
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error getting the observability health data", slog.String("errorMessage", err.Error()))
			observStatus.Error = err.Error()
		}

		status := healthStatus{
			DBHealth:     dbStatus,
			Healthy:      dbStatus.Healthy && observStatus.Healthy,
			ObservHealth: observStatus,
		}

		defer func() {
			if fail := recover(); fail != nil {
				svr.Logger.ErrorContext(ctx, "Fatal error doing an application health check.", slog.Any("errorMessage", fail))
				dbStatus.Error = fmt.Sprintf("%v", fail)
			}
		}()

		tmpl, tmplErr := template.ParseFiles(svr.Getenv("TEMPLATES_DIR") + "/health.html")

		healthCheckCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.Bool("healthy", status.Healthy),
			attribute.Bool("dbHealthy", status.DBHealth.Healthy),
			attribute.Bool("observHealthy", status.ObservHealth.Healthy),
		))

		span.SetAttributes(
			attribute.Bool("healthy", status.Healthy),
			attribute.Bool("dbHealthy", status.DBHealth.Healthy),
			attribute.String("dbError", status.DBHealth.Error),
		)

		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s", req.URL.Path),
			slog.Bool("healthy", status.Healthy),
			slog.Bool("dbHealthy", status.DBHealth.Healthy),
			slog.Bool("observHealthy", status.ObservHealth.Healthy),
			slog.String("dbError", status.DBHealth.Error),
			slog.String("observError", status.ObservHealth.Error),
		)

		if tmplErr != nil {
			svr.Logger.ErrorContext(ctx,
				"Error loading the health check template",
				slog.String("errorMessage", tmplErr.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering the health check page"))
			return
		}

		svr.Logger.DebugContext(ctx,
			"Writing health check results",
			slog.Any("results", status),
		)
		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "health", status)
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error writing health check template!",
				slog.String("errorMessage", err.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading health dashboard"))
			return
		}

	})

}

func dbHealth(ctx context.Context, db *sql.DB) (healthInfo, error) {

	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	stats := healthInfo{
		Healthy: false,
	}

	/* Ping the database */
	err := db.PingContext(ctx)
	if err != nil {
		stats.Healthy = false
		stats.Error = fmt.Sprintf("db down: %v", err)
		return stats, fmt.Errorf("error pinging the database to confirm it's up: %s", err.Error())
	}

	/* Database is up, add more statistics */
	stats.Healthy = true

	return stats, nil

}

// Hits the health check endpoint of my obersvability tool and confirms it's up
// Returns a struct with the status and any other details I want to include
func observHealth(getenv func(string) string, logger *slog.Logger) (healthInfo, error) {

	observHealth := healthInfo{
		Healthy: false,
	}

	res, err := http.Get(getenv("OTEL_HC"))
	if err != nil {
		return observHealth, fmt.Errorf("error reading the observability health check endpoint: %s", err.Error())
	}
	defer res.Body.Close()

	jsonData := make(map[string]any)
	err = json.NewDecoder(res.Body).Decode(&jsonData)
	if err != nil {
		return observHealth, fmt.Errorf("error reading the observability health status: %s", err.Error())
	}

	/*
		The response appears to be just a field indicating if the observability tool
		can connect to its datastore, so we'll look that up and use it.
	*/
	observHealth.Healthy = strings.ToLower(jsonData["database"].(string)) == "ok"
	return observHealth, nil

}
