package server

import (
	"gift-registry/internal/util"
	"html/template"
	"log/slog"
	"net/http"
)

// Returns the landing page for the application
func IndexHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

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
