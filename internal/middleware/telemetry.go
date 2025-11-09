package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"gift-registry/internal/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type attributesKey int

type responseWithStatus struct {
	responseWriter http.ResponseWriter
	statusCode     int
}

const (
	name = "net.hydrick.gift-registry"
)

const (
	_ attributesKey = iota
	attrKey
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

		ctx, span := tracer.Start(req.Context(),
			fmt.Sprintf("HTTP %s %s", req.Method, req.URL.Path))
		defer span.End()

		attributes := []attribute.KeyValue{}
		ctx = context.WithValue(ctx, attrKey, attributes)
		req = req.WithContext(ctx)

		statRes := wrapResponseWriter(res)

		next.ServeHTTP(statRes, req)

		// attributes, _ = ctx.Value(attrKey).([]attribute.KeyValue)
		attributes = append(attributes,
			attribute.Bool("successful", statRes.statusCode >= 200 && statRes.statusCode < 300))
		attributes = append(attributes, attribute.String("path", req.URL.Path))
		attributes = append(attributes, attribute.Int("statusCode", statRes.statusCode))

		counter.Add(ctx, 1, metric.WithAttributes(attributes...))

		span.SetAttributes(attributes...)

		/* Convert our span attributes to other types of attributes for a canonical log line */
		logAttrs := make([]any, len(attributes))

		for _, attr := range attributes {
			logAttrs = append(logAttrs, slog.Any(string(attr.Key), attr.Value))
		}

		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s", req.URL.Path),
			logAttrs...,
		)

	})
}

func TelemetryAttributes(ctx context.Context) []attribute.KeyValue {
	attributes, ok := ctx.Value(attrKey).([]attribute.KeyValue)

	/* Default to an empty attribute list instead of returning that there aren't any attributes */
	if !ok {
		attributes = []attribute.KeyValue{}
	}
	return attributes
}

func WriteTelemetry(ctx context.Context, attributes []attribute.KeyValue) context.Context {
	return context.WithValue(ctx, attrKey, attributes)
}

func wrapResponseWriter(res http.ResponseWriter) *responseWithStatus {
	return &responseWithStatus{
		responseWriter: res,
		statusCode:     0,
	}
}

func (rs *responseWithStatus) Done() {
	/* Default the status to a 200 OK */
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
