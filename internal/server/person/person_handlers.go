package person

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"

	"gift-registry/internal/server"

	"go.opentelemetry.io/otel"
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
	meter  = otel.Meter(name)
	tracer = otel.Tracer(name)
)

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
