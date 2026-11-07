package templates

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/*
var templatesDir embed.FS

var commonComponents []string
var logger *slog.Logger

func init() {

	commonComponents = []string{"templates/header.html"}

}

func Initialize(appLogger *slog.Logger) {

	logger = appLogger

}

// WriteResponsePage creates a top-level page template, with any common page
// components the site uses, then renders the template using
// WriteResponseTemplate().
func WriteResponsePage(
	ctx context.Context,
	res http.ResponseWriter,
	statusCode int,
	name string,
	pages []string,
	pageContent any,
	funcs template.FuncMap,
) error {

	pages = append(pages, commonComponents...)
	funcs["dict"] = func(values ...any) map[string]any {

		outputDict := map[string]any{}

		for idx := 0; idx < len(values)-1; idx += 2 {

			outputDict[values[idx].(string)] = values[idx+1]

		}

		return outputDict

	}

	return WriteResponseTemplate(ctx, res, statusCode, name, pages, pageContent, funcs)

}

// WriteResponseTemplate loads the given template(s) and executes them to
// send HTML back to the caller. If for any reason there's a problem,
// and HTTP error response with a generic error message.
func WriteResponseTemplate(
	ctx context.Context,
	res http.ResponseWriter,
	statusCode int,
	name string,
	pages []string,
	pageContent any,
	funcs template.FuncMap,
) error {

	tmpl, err := template.New(name).Funcs(funcs).ParseFS(templatesDir, pages...)
	if err != nil {
		res.WriteHeader(500)
		res.Write([]byte("Error loading the page template"))
		return fmt.Errorf("could not construct template(s): %v", err)
	}

	res.WriteHeader(statusCode)
	err = tmpl.ExecuteTemplate(res, name, pageContent)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Error writing template",
			slog.String("templateName", name),
			slog.String("errorMessage", err.Error()),
		)
		res.WriteHeader(500)
		res.Write([]byte("Error populating page data"))
	}
	return err

}
