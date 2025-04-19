package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type healthStatus struct {
	DBHealth     dbHealthInfo
	Healthy      bool
	ObservHealth observHealthInfo
}

type dbHealthInfo struct {
	IdleConnections   int
	ConnectionsInUse  int
	Error             string
	Healthy           bool
	MaxIdleConnClosed int64
	MaxLifetimeClosed int64
	Message           string
	OpenConnections   int
	Status            string
	WaitCount         int64
	WaitDuration      time.Duration
}

type observHealthInfo struct {
	Error   string
	Healthy bool
	Status  string
}

const (
	name = "net.hydrick.gift-registry/server"
)

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
func HealthCheckHandler(getenv func(string) string, db *sql.DB, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "health")
		defer span.End()

		responseStatus := 200

		dbStatus, err := dbHealth(db)
		logger.DebugContext(ctx, "DB status info obtained", slog.Any("statusObj", dbStatus))
		if err != nil {
			logger.ErrorContext(ctx, "Error getting database health data", slog.String("errorMessage", err.Error()))
			dbStatus.Error = err.Error()
		}

		observStatus, err := observHealth(getenv)
		logger.DebugContext(ctx, "Observability health info obtained", slog.Any("statusObj", observStatus))
		if err != nil {
			logger.ErrorContext(ctx, "Error getting the observability health data", slog.String("errorMessage", err.Error()))
			observStatus.Error = err.Error()
		}

		status := healthStatus{
			DBHealth:     dbStatus,
			Healthy:      dbStatus.Healthy && observStatus.Healthy,
			ObservHealth: observStatus,
		}

		defer func() {
			if fail := recover(); fail != nil {
				logger.ErrorContext(ctx, "Fatal error doing an application health check.", slog.Any("errorMessage", fail))
				responseStatus = 500
				res.WriteHeader(responseStatus)
				dbStatus.Error = fmt.Sprintf("%v", fail)
			}
		}()

		healthCheckCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.Bool("healthy", status.Healthy),
			attribute.Bool("dbHealthy", status.DBHealth.Healthy),
			attribute.Int64("dbOpenConnections", int64(status.DBHealth.OpenConnections)),
			attribute.Int64("dbConnectionsInUse", int64(status.DBHealth.ConnectionsInUse)),
			attribute.Int64("dbIdleConnections", int64(status.DBHealth.IdleConnections)),
			attribute.Int64("dbWaitDuration", int64(status.DBHealth.WaitDuration)),
			attribute.Bool("observHealthy", status.ObservHealth.Healthy),
		))

		span.SetAttributes(
			attribute.Bool("healthy", status.Healthy),
			attribute.Bool("dbHealthy", status.DBHealth.Healthy),
			attribute.String("dbMessage", status.DBHealth.Message),
			attribute.String("dbError", status.DBHealth.Error),
			attribute.String("dbStatus", status.DBHealth.Status),
			attribute.Int64("dbOpenConnections", int64(status.DBHealth.OpenConnections)),
			attribute.Int64("dbConnectionsInUse", int64(status.DBHealth.ConnectionsInUse)),
			attribute.Int64("dbIdleConnections", int64(status.DBHealth.IdleConnections)),
			attribute.Int64("dbWaitDuration", int64(status.DBHealth.WaitDuration)),
			attribute.Int64("dbMaxIdleConnClosed", int64(status.DBHealth.MaxIdleConnClosed)),
			attribute.Int64("dbMaxLifetimeClosed", int64(status.DBHealth.MaxLifetimeClosed)),
			attribute.Bool("observHealthy", status.ObservHealth.Healthy),
			attribute.String("observStatus", status.ObservHealth.Status),
		)

		tmpl := template.Must(template.ParseFiles(getenv("TEMPLATES_DIR") + "/health.html"))

		logger.InfoContext(ctx, fmt.Sprintf("Finished the %s operation", req.URL.Path),
			slog.Bool("healthy", status.Healthy),
			slog.Bool("dbHealthy", status.DBHealth.Healthy),
			slog.Bool("observHealthy", status.ObservHealth.Healthy),
			slog.String("dbMessage", status.DBHealth.Message),
			slog.String("dbError", status.DBHealth.Error),
			slog.String("dbStatus", status.DBHealth.Status),
		)
		res.WriteHeader(responseStatus)
		tmpl.Execute(res, status)

	})

}

func dbHealth(db *sql.DB) (dbHealthInfo, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := dbHealthInfo{
		IdleConnections:   0,
		ConnectionsInUse:  0,
		Error:             "",
		Healthy:           false,
		MaxIdleConnClosed: 0,
		Message:           "",
		OpenConnections:   0,
		Status:            "",
		WaitCount:         0,
		WaitDuration:      0,
	}

	// Ping the database
	err := db.PingContext(ctx)
	if err != nil {
		stats.Status = "down"
		stats.Healthy = false
		stats.Error = fmt.Sprintf("db down: %v", err)
		return stats, err
	}

	// Database is up, add more statistics
	stats.Status = "Up"
	stats.Healthy = true
	stats.Message = "Database healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := db.Stats()
	stats.OpenConnections = dbStats.OpenConnections
	stats.ConnectionsInUse = dbStats.InUse
	stats.IdleConnections = dbStats.Idle
	stats.WaitCount = dbStats.WaitCount
	stats.WaitDuration = dbStats.WaitDuration
	stats.MaxIdleConnClosed = dbStats.MaxIdleClosed
	stats.MaxLifetimeClosed = dbStats.MaxLifetimeClosed

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats.Message = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats.Message = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats.Message = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats.Message = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats, nil

}

func observHealth(getenv func(string) string) (observHealthInfo, error) {

	observHealth := observHealthInfo{
		Healthy: false,
		Status:  "unhealthy",
	}

	res, err := http.Get(getenv("OTEL_HC"))
	if err != nil {
		return observHealth, err
	}
	defer res.Body.Close()

	jsonData := make(map[string]any)
	err = json.NewDecoder(res.Body).Decode(&jsonData)
	if err != nil {
		return observHealth, err
	}

	observHealth.Status = jsonData["database"].(string)
	observHealth.Healthy = strings.ToLower(observHealth.Status) == "ok"

	return observHealth, nil

}
