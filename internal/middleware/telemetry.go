package middleware

import (
	"fmt"
	"log/slog"
	"net/http"

	"gift-registry/internal/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// type attributesKey int

const (
	name = "net.hydrick.gift-registry/middleware"
)

var (
	meter   = otel.Meter(name)
	tracer  = otel.Tracer(name)
	counter metric.Int64Counter
)

func init() {
	var err error
	counter, err = meter.Int64Counter(
		"endpoint_counter",
		metric.WithDescription("Number of times an endpoint is hit"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		panic(err)
	}

}

func Telemetry(svr *util.ServerUtils, next http.Handler) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), req.URL.Path)
		defer span.End()

		svr.Logger.DebugContext(
			ctx,
			"STARTED A TRACE",
			slog.String("traceName", req.URL.Path),
		)
		span.SetAttributes(attribute.String("path", req.URL.Path))
		req = req.WithContext(ctx)

		next.ServeHTTP(res, req)
		counter.Add(ctx, 1)

		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s %s", req.Method, req.URL.Path),
		)

	})
}
