package person

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"

	"gift-registry/internal/server"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type registration struct {
	email     string
	firstName string
	lastName  string
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

// Creates a new user account in the person table. The signup is valid if the user has provided a properly formatted email address, a first name, and a last name (we're not tracking any other user details).
func SignupHandler(svr server.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "health")
		defer span.End()

		signup := registration{
			email:     req.PostFormValue("email"),
			firstName: req.PostFormValue("firstName"),
			lastName:  req.PostFormValue("lastName"),
		}

		validationErrors := signup.validate()
		if validationErrors != nil {
			/*
				TODO:
				SEND BACK THE FORM WITH THE ERROR MESSAGE(S) FOR FIXING THE VALIDATIONe
				ISSUES
			*/
		}

		/* TODO: CREATE A NEW ACCOUNT IF THE FORM DATA IS VALID */

		/*
			TODO:
			WILL NEED TO CAPTURE THESE FOR THE FAILURE CASE ABOVE
		*/
		signupCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.Bool("successful", true),
		))

		/*
			I'm not wild about capturing names in the trace, but it gives me a hook for
			debugging. In a "proper" cloud app, email would be higher cardinality and
			give a higher chance for anonymity (if a person used an email account that
			didn't have their name in it). Given that this app is scoped as a
			self-hosted app for a family, I'm going to assume anyone looking at the
			logs would recognize the emails anywayS.
		*/
		/*
			TODO: NEED TO FIGURE OUT A WAY TO LOCK OUT THE OBSERVABILITY DATA IN GRAFANA TO MAKE SURE ONLY I SEE IT.
		*/
		span.SetAttributes(
			attribute.Bool("successful", true),
			attribute.String("firstName", signup.firstName),
			attribute.String("lastName", signup.lastName),
		)
		/* TODO: SHOULD I MOVE THESE MESSAGES TO A MIDDLEWARE WITH A CONTEXT VARAIBLE REFERENCE FOR DATA? */
		svr.Logger.InfoContext(ctx, fmt.Sprintf("Finished the operation %s", req.URL.Path), slog.Bool("validationErrors", validationErrors != nil), slog.String("validationMessages", validationErrors.Error()))

	})

}

func (reg registration) validate() error {

	errs := make([]error, 0)

	if _, err := mail.ParseAddress(reg.email); err != nil {

		errs = append(errs, fmt.Errorf("invalid email address: %s", err.Error()))

	}

	if reg.firstName == "" {

		errs = append(errs, fmt.Errorf("first name is a required field"))

	}

	if reg.lastName == "" {

		errs = append(errs, fmt.Errorf("last name is a required field"))

	}

	return errors.Join(errs...)

}
