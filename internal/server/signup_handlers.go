package person

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"text/template"

	"gift-registry/internal/server"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type signupForm struct {
	Email      string
	Errors     signupFormErrors
	FirstName  string
	LastName   string
	successful bool
}

type signupFormErrors struct {
	Email        string
	ErrorMessage string
	FirstName    string
	LastName     string
}

const (
	name = "net.hydrick.gift-registry/server"
)

var (
	meter     = otel.Meter(name)
	tracer    = otel.Tracer(name)
	signupCtr metric.Int64Counter
)

func init() {

	var err error
	signupCtr, err = meter.Int64Counter(
		"signup.counter",
		metric.WithDescription("Number of signup attempts"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		panic(err)
	}

}

// Creates a new user account in the person table. The signup is valid if the
// user has provided a properly formatted email address, a first name, and a
// last name (we're not tracking any other user details).
func SignupHandler(svr server.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "health")
		defer span.End()

		userData := signupForm{
			Email:      req.PostFormValue("email"),
			FirstName:  req.PostFormValue("firstName"),
			LastName:   req.PostFormValue("lastName"),
			successful: true,
		}

		userData.validate()

		/*
			Send the user details back to leave the form populated, but add error
			messaging (also capture the associated telemetry)
		*/
		if userData.Errors.Email != "" ||
			userData.Errors.FirstName != "" ||
			userData.Errors.LastName != "" {

			userData.successful = false
			signupResponse(ctx, span, res, req, svr, userData)
			return

		}

		/* Open a transaction so we can rollback on a DB write failure */
		tx, err := svr.DB.BeginTx(ctx, nil)
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error starting transaction", slog.String("errorMessage", err.Error()))
			tx.Rollback()
			userData.successful = false
		}

		_, err = svr.DB.ExecContext(ctx, "INSERT INTO gift_registry.person (firstName, lastName, email) VALUES ($1, $2, $3)", userData.FirstName, userData.LastName, userData.Email)
		if err != nil {
			svr.Logger.ErrorContext(ctx, fmt.Sprintf("Error adding person %s %s to the database", userData.FirstName, userData.LastName))
			userData.successful = false
			rbErr := tx.Rollback()
			if rbErr != nil {
				svr.Logger.ErrorContext(ctx, "Error rolling back the transaction", slog.String("rollbackErrorMsg", rbErr.Error()))
				userData.Errors.ErrorMessage = "Critical database error"
				signupResponse(ctx, span, res, req, svr, userData)
				/* The database may be in an invalid state here, panic to force manual intervention */
				panic(rbErr)
			}
			userData.Errors.ErrorMessage = "Database error"
			signupResponse(ctx, span, res, req, svr, userData)
			return
		}

		tx.Commit()
		signupResponse(ctx, span, res, req, svr, userData)

	})

}

func SignupFormHandler(svr server.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "signupForm")
		defer span.End()

		svr.Logger.InfoContext(ctx, fmt.Sprintf("Finished the operation %s", req.URL.Path))

		dir := svr.Getenv("TEMPLATES_DIR")
		tmpl, tmplErr := template.ParseFiles(dir + "/signup-form.html")

		if tmplErr != nil {
			svr.Logger.ErrorContext(ctx, "Error loading the signup form template", slog.String("errorMessage", tmplErr.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry signup"))
			return
		}

		res.WriteHeader(200)

		err := tmpl.ExecuteTemplate(res, "index", signupForm{})
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error writing template!",
				slog.String("errorMessage", err.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry"))
			return
		}

	})

}

func signupResponse(ctx context.Context,
	span trace.Span,
	res http.ResponseWriter,
	req *http.Request,
	svr server.ServerUtils,
	userData signupForm) {

	templates := svr.Getenv("TEMPLATES_DIR")
	signupCtr.Add(ctx, 1, metric.WithAttributes(
		attribute.Bool("successful", userData.successful),
	))
	span.SetAttributes(
		attribute.Bool("successful", false),
		attribute.String("firstName", userData.FirstName),
		attribute.String("lastName", userData.LastName),
	)
	tmpl, tmplErr := template.ParseFiles(templates+"/index.html", templates+"/signup-form.html", templates+"/login-form.html")
	if tmplErr != nil {
		res.WriteHeader(500)
		res.Write([]byte("Error loading the signup page template!"))
		return
	}

	/*
		I'm not wild about capturing names in the trace, but it gives me a hook for
		debugging. In a "proper" cloud app, email would be higher cardinality and
		give a higher chance for anonymity (if a person used an email account that
		didn't have their name in it). Given that this app is scoped as a
		self-hosted app for a family, I'm not going to worry about the potential
		for overlap when searching on names.
	*/
	span.SetAttributes(
		attribute.Bool("successful", userData.successful),
		attribute.String("firstName", userData.FirstName),
		attribute.String("lastName", userData.LastName),
	)

	/* TODO: SHOULD I MOVE THESE MESSAGES TO A MIDDLEWARE WITH A CONTEXT VARAIBLE REFERENCE FOR DATA? */
	svr.Logger.InfoContext(ctx,
		fmt.Sprintf("Finished the operation %s", req.URL.Path),
		slog.String("userData", userData.String()),
		slog.String("validationErrors", userData.Error()))

	res.WriteHeader(200)
	tmpl.Execute(res, userData)

}

func (sf *signupForm) validate() {

	if _, err := mail.ParseAddress(strings.Trim(sf.Email, " ")); err != nil {

		sf.Errors.Email = "Invalid email address"

	}

	if strings.Trim(sf.FirstName, " ") == "" {

		sf.Errors.FirstName = "First name is required"

	}

	if strings.Trim(sf.LastName, " ") == "" {

		sf.Errors.LastName = "Last name is required"

	}

}

func (sf signupForm) String() string {

	return fmt.Sprintf("firstName=%s, lastName=%s", sf.FirstName, sf.LastName)

}

func (sf signupForm) Error() string {

	return fmt.Sprintf("email=%s, firstName=%s, lastName=%s", sf.Errors.Email, sf.Errors.FirstName, sf.Errors.LastName)

}
