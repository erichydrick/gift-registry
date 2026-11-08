package templates

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

//go:embed templates/*
var templatesDir embed.FS

var commonComponents []string
var commonFuncMaps template.FuncMap
var htmlTemplates *template.Template
var logger *slog.Logger

/*
Define common HTML templates and functions that are available to all
page-level templates, parse the templates directory once, and define common
function maps available to all page-level templates.
*/
func init() {

	commonComponents = []string{"header.html"}
	commonFuncMaps = template.FuncMap{
		"dict": func(values ...any) map[string]any {

			outputDict := map[string]any{}

			for idx := 0; idx < len(values)-1; idx += 2 {

				outputDict[values[idx].(string)] = values[idx+1]

			}

			return outputDict

		},
		"editable": func(editableLists []string, id string) bool {
			return slices.Contains(editableLists, id)
		},
		"formatDate": func(datetime string) string {
			if parsed, err := time.Parse("2006-01-02", datetime); err != nil {
				return datetime
			} else {
				return parsed.Format("01/02/06")
			}
		},
		"maxVal": func(leftNum int, rightNum int) int {
			return max(leftNum, rightNum)
		},
		"subtract": func(requested int8, claimed int8) int8 {
			return requested - claimed
		},
	}
	htmlTemplates = template.Must(template.New("").
		Funcs(commonFuncMaps).
		ParseFS(templatesDir, "templates/*.html"))

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
) error {

	pages = append(pages, commonComponents...)

	return WriteResponseTemplate(ctx, res, statusCode, name, pages, pageContent)

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
) error {

	res.WriteHeader(statusCode)
	err := htmlTemplates.ExecuteTemplate(res, name, pageContent)
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
