package registry

import (
	"gift-registry/internal/util"
	"html/template"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Returns the registry items, grouped by person
func RegistryHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("registry_handler")

		templatesDir := svr.Getenv("TEMPLATES_DIR")
		tmpl, err := template.ParseFiles(templatesDir + "/registry_page.html" /* TODO: REGISTRY FORM IN A SUB-TEMPLATE?*/)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading registry template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering the profile page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		/* TODO: QUERY THE DB, POPULATE THE REGISTRIES, RETURN */

		res.WriteHeader(200)
		/* TODO: ADD REGISTRIES AS PARAM WHEN I QUERY THEM */
		err = tmpl.ExecuteTemplate(res, "registry-page", nil)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", errorMessage),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading registry page"))
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}

	})

}
