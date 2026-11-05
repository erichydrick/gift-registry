package templates

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
)

//go:embed templates/*
var templatesDir embed.FS

var commonComponents []string

func init() {

	commonComponents = []string{"templates/header.html"}

}

// TODO: WRITE ERROR

// WriteResponsePage creates a top-level page template, with any common page
// components the site uses, then renders the template using
// WriteResponseTemplate().
func WriteResponsePage(
	res http.ResponseWriter,
	statusCode int,
	name string,
	pages []string,
	pageContent any,
	funcs template.FuncMap,
) error {

	pages = append(pages, commonComponents...)
	return WriteResponseTemplate(res, statusCode, name, pages, pageContent, funcs)

}

// WriteResponseTemplate loads the given template(s) and executes them to
// send HTML back to the caller. If for any reason there's a problem,
// and HTTP error response with a generic error message.
func WriteResponseTemplate(
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
		res.WriteHeader(500)
		res.Write([]byte("Error populating page data"))
	}
	return err

}
