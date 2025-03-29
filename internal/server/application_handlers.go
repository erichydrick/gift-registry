package server

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

type healthStatus struct {
	DBHealth dbHealthInfo
	Healthy  bool
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

// Checks the health of the application and returns some relevant statistics
func HealthCheckHandler(templatesDir string, db *sql.DB, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		responseStatus := 200

		dbStatus, err := dbHealth(db)
		if err != nil {
			logger.Error("Error getting database health data", slog.String("errorMessage", err.Error()))
			responseStatus = 500
			/* TODO: Make an error page and render it */
		}

		status := healthStatus{
			DBHealth: dbStatus,
			Healthy:  dbStatus.Error == "",
		}

		/*
			TODO: WRITE A LIST OF ATTRIBUTES THAT I CAN ADD THE ERROR MESSAGE AND DB HEALTH TO, THEN JUST ALWAYS LOG THE ATTR LIST
		*/
		defer func() {
			if fail := recover(); fail != nil {
				logger.Error("Fatal error doing an application health check.", slog.Any("errorMessage", fail))
				responseStatus = 500
				res.WriteHeader(responseStatus)
				/* TODO: MAKE THIS A ERROR HTML SNIPPET */
				res.Write([]byte{})
			}
		}()
		tmpl := template.Must(template.ParseFiles(templatesDir + "/health.html"))

		logger.Info("Canonical log line for application health check.",
			slog.Bool("healthy", status.Healthy),
			slog.Any("details", status))
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
