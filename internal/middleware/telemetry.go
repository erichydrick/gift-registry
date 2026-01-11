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

// TODO: DO I NEED THIS?
/*
type responseWithStatus struct {
	responseWriter http.ResponseWriter
	statusCode     int
}
*/

const (
	name = "net.hydrick.gift-registry"
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

		ctx, span := tracer.Start(req.Context(), "TelemetryMiddleware")
		defer span.End()

		span.SetAttributes(attribute.String("path", req.URL.Path))
		req = req.WithContext(ctx)

		// TODO: DO I WANT THIS?
		// statRes := wrapResponseWriter(res)

		next.ServeHTTP(res, req)
		// next.ServeHTTP(statRes, req)

		// span.SetAttributes(attribute.Int("statusCode", statRes.statusCode))

		// TODO: WHAT ATTRIBUTES DO I WANT TO THROW ON HERE?
		counter.Add(ctx, 1)
		// counter.Add(ctx, 1, metric.WithAttributes(attributes...))

		/*
			Convert our span attributes to other types of attributes for a canonical log line
		*/
		/*
			logAttrs := make([]any, len(attributes))

			for _, attr := range attributes {
				logAttrs = append(logAttrs, slog.Any(string(attr.Key), attr.Value))
			}
		*/
		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s %s", req.Method, req.URL.Path),
		)

	})
}

/*
func wrapResponseWriter(res http.ResponseWriter) *responseWithStatus {
	return &responseWithStatus{
		responseWriter: res,
		statusCode:     0,
	}
}

func (rs *responseWithStatus) Done() {
	// Default the status to a 200 OK
	if rs.statusCode == 0 {
		rs.statusCode = 200
	}
}

func (rs *responseWithStatus) Header() http.Header {
	return rs.responseWriter.Header()
}

func (rs *responseWithStatus) Write(buf []byte) (int, error) {
	return rs.responseWriter.Write(buf)
}

func (rs *responseWithStatus) WriteHeader(statusCode int) {
	rs.statusCode = statusCode
	rs.responseWriter.WriteHeader(statusCode)
}
*/
