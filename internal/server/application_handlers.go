package server

import (
	"gift-registry/internal/templates"
	"gift-registry/internal/util"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

// Returns the landing page for the application
func IndexHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("index_handler")

		_ = templates.WriteResponsePage(
			ctx,
			res,
			200,
			"index",
			[]string{"index.html"},
			"",
		)

	})

}
