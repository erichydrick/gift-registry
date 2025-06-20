package server

import (
	"fmt"
	"gift-registry/internal/util"
	"html/template"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel"
)

const (
	name = "net.hydrick.gift-registry/server"
)

var (
	meter  = otel.Meter(name)
	tracer = otel.Tracer(name)
)

// Returns the landing page for the application
func IndexHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "index")
		defer span.End()

		svr.Logger.InfoContext(ctx, fmt.Sprintf("Finished the operation %s", req.URL.Path))

		dir := svr.Getenv("TEMPLATES_DIR")
		tmpl, tmplErr := template.ParseFiles(dir + "/index.html")

		if tmplErr != nil {
			svr.Logger.ErrorContext(ctx, "Error loading the index template", slog.String("errorMessage", tmplErr.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry"))
			return
		}

		res.WriteHeader(200)

		err := tmpl.ExecuteTemplate(res, "index", "")
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error writing template!",
				slog.String("errorMessage", err.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry"))
			return
		}

	})

}
