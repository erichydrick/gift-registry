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

type registration struct {
	ErrorMessage   string
	Email          string
	EmailError     string
	FirstName      string
	FirstNameError string
	LastName       string
	LastNameError  string
	successful     bool
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

		userData := registration{
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
		if userData.EmailError != "" ||
			userData.FirstNameError != "" ||
			userData.LastNameError != "" {

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
				userData.ErrorMessage = "Critical database error"
				signupResponse(ctx, span, res, req, svr, userData)
				/* The database may be in an invalid state here, panic to force manual intervention */
				panic(rbErr)
			}
			userData.ErrorMessage = "Database error"
			signupResponse(ctx, span, res, req, svr, userData)
			return
		}

		tx.Commit()
		signupResponse(ctx, span, res, req, svr, userData)

	})

}

func signupResponse(ctx context.Context,
	span trace.Span,
	res http.ResponseWriter,
	req *http.Request,
	svr server.ServerUtils,
	userData registration) {

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

func (reg *registration) validate() {

	if _, err := mail.ParseAddress(strings.Trim(reg.Email, " ")); err != nil {

		reg.EmailError = "Invalid email address"

	}

	if strings.Trim(reg.FirstName, " ") == "" {

		reg.FirstNameError = "First name is required"

	}

	if strings.Trim(reg.LastName, " ") == "" {

		reg.LastNameError = "Last name is required"

	}

}

func (re registration) String() string {

	return fmt.Sprintf("firstName=%s, lastName=%s", re.FirstName, re.LastName)

}

func (re registration) Error() string {

	return fmt.Sprintf("email=%s, firstName=%s, lastName=%s", re.EmailError, re.FirstNameError, re.LastNameError)

}
