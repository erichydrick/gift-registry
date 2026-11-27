package middleware

import (
	"fmt"
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

		ctx := req.Context()

		next.ServeHTTP(res, req)
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("pattern", req.Pattern)))

		svr.Logger.InfoContext(
			ctx,
			fmt.Sprintf("Finished the operation %s %s", req.Method, req.URL.Path),
		)

	})
}
