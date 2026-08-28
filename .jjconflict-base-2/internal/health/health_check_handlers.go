package health

import (
	"context"
	"fmt"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type healthStatus struct {
	DBHealth healthInfo
	Healthy  bool
}

type healthInfo struct {
	Error   string
	Healthy bool
}

// Checks the health of the application and returns some relevant statistics
func HealthCheckHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("health_check")

		dbStatus, err := dbHealth(ctx, svr)
		svr.Logger.DebugContext(ctx, "DB status info obtained", slog.Any("statusObj", dbStatus))
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error getting database health data", slog.String("errorMessage", err.Error()))
			dbStatus.Error = err.Error()
			span.SetAttributes(attribute.String("error_message", err.Error()))
		}

		status := healthStatus{
			DBHealth: dbStatus,
			Healthy:  dbStatus.Healthy,
		}

		defer func() {
			if fail := recover(); fail != nil {
				svr.Logger.ErrorContext(ctx, "Fatal error doing an application health check.", slog.Any("errorMessage", fail))
				dbStatus.Error = fmt.Sprintf("%v", fail)
			}
		}()

		span.SetAttributes(
			attribute.Bool("healthy", status.Healthy),
			attribute.Bool("dbHealthy", status.DBHealth.Healthy),
			attribute.String("dbError", status.DBHealth.Error),
		)

		tmpl, tmplErr := template.ParseFiles(svr.Getenv("TEMPLATES_DIR") + "/health.html")

		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s", req.URL.Path),
			slog.Bool("healthy", status.Healthy),
			slog.Bool("dbHealthy", status.DBHealth.Healthy),
			slog.String("dbError", status.DBHealth.Error),
		)

		if tmplErr != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading the health check template",
				slog.String("errorMessage", tmplErr.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering the health check page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		svr.Logger.DebugContext(
			ctx,
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
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

	})

}

func dbHealth(ctx context.Context, svr *util.ServerUtils) (healthInfo, error) {

	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	stats := healthInfo{
		Healthy: false,
	}

	/* Ping the database */
	err := svr.DB.Ping(ctx)
	if err != nil {
		stats.Healthy = false
		stats.Error = fmt.Sprintf("db down: %v", err)
		return stats, fmt.Errorf("error pinging the database to confirm it's up: %s", err.Error())
	}

	/* Database is up, add more statistics */
	stats.Healthy = true

	return stats, nil

}
