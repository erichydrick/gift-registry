package profile

import (
	"fmt"
	"gift-registry/internal/middleware"
	"gift-registry/internal/util"
	"html/template"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
)

type profileErrors struct {
	Email        string
	ErrorMessage string
	FirstName    string
	LastName     string
}

type userData struct {
	DisplayName string
	Errors      profileErrors
	Email       string
	FirstName   string
	LastName    string
	personID    int64
	valid       bool
}

const (
	lookupPersonQuery = "SELECT person_id, email, first_name, last_name, display_name FROM person WHERE person_id = $1"
	updatePersonQuery = "UPDATE person SET email = $1, first_name = $2, last_name = $3, display_name = $4 WHERE person_id = $5"
	varcharMaxLength  = 255
)

// Looks up the person information and returns it.
func ProfileHandler(svr *util.ServerUtils) http.HandlerFunc {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		var user userData
		personID := middleware.PersonID(res, req)
		svr.DB.QueryRow(ctx, lookupPersonQuery, personID).Scan(&user.personID, &user.Email, &user.FirstName, &user.LastName, &user.DisplayName)

		/*
			By default we display people by first name, but that can be overridden in
			the database with something like a "grandparent name"
		*/
		if user.DisplayName == "" {
			user.DisplayName = user.FirstName
		}

		attributes := middleware.TelemetryAttributes(ctx)
		attributes = append(attributes, attribute.Int64("personID", user.personID))
		attributes = append(attributes, attribute.String("firstName", user.FirstName))
		attributes = append(attributes, attribute.String("lastName", user.LastName))
		attributes = append(attributes, attribute.String("displayName", user.DisplayName))
		ctx = middleware.WriteTelemetry(ctx, attributes)
		_ = req.WithContext(ctx)

		templatesDir := svr.Getenv("TEMPLATES_DIR")
		tmpl, err := template.ParseFiles(templatesDir+"/profile_page.html", templatesDir+"/profile_form.html")
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading the profile page template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering the profile page"))
			return
		}

		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "profile-page", user)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading your profile page"))
			return
		}

	})

}

// Updates the person's information with the values provided from form input.
func ProfileUpdateHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		attributes := middleware.TelemetryAttributes(ctx)
		personID := middleware.PersonID(res, req)

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error parsing the profile update form",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(400)
			res.Write([]byte("Could not user data"))
			return
		}

		user := userData{
			DisplayName: req.FormValue("displayName"),
			Email:       req.FormValue("email"),
			FirstName:   req.FormValue("firstName"),
			LastName:    req.FormValue("lastName"),
		}

		attributes = append(attributes, attribute.Int64("personID", personID))
		attributes = append(attributes, attribute.String("updatedDisplayName", user.DisplayName))
		attributes = append(attributes, attribute.String("updatedEmail", user.Email))
		attributes = append(attributes, attribute.String("updatedFirstName", user.FirstName))
		attributes = append(attributes, attribute.String("updatedLastName", user.LastName))

		tmpl, err := template.ParseFiles(svr.Getenv("TEMPLATES_DIR") + "/profile_form.html")
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading the profile page template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading the profile page template!"))
			return
		}

		/*
			We should always have a display name, so when in doubt use first name
		*/
		if user.DisplayName == "" {

			user.DisplayName = user.FirstName

		}

		user.validate()
		attributes = append(attributes, attribute.Bool("dataValid", user.valid))
		ctx = middleware.WriteTelemetry(ctx, attributes)
		_ = req.WithContext(ctx)

		if !user.valid {

			res.WriteHeader(200)
			err = tmpl.ExecuteTemplate(res, "profile-form", user)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error writing the profile page error messages",
					slog.String("errorMessage", err.Error()),
				)
				/*
					We're returning early error or no, so don't need a return statement here
				*/

			}

			return
		}

		_, err = svr.DB.Execute(ctx, updatePersonQuery, user.Email, user.FirstName, user.LastName, user.DisplayName, personID)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error updating the profile information",
				slog.String("errorMessage", err.Error()),
			)

			user.Errors.ErrorMessage = "Could not save the profile update"
			err = tmpl.ExecuteTemplate(res, "profile-page", user)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error writing the profile page error messages",
					slog.String("errorMessage", err.Error()),
				)
				/*
					We're returning early error or no, so don't need a return statement here
				*/

			}

			return

		}

		/* On save, return the form with the updated database values */
		svr.DB.QueryRow(ctx, lookupPersonQuery, personID).Scan(&user.personID, &user.Email, &user.FirstName, &user.LastName, &user.DisplayName)
		err = tmpl.ExecuteTemplate(res, "profile-form", user)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error reading updated values into response",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading your profile page"))
			return
		}

	})

}

func (user *userData) validate() {

	user.valid = true

	if user.Email == "" || len(user.Email) > varcharMaxLength {

		user.Errors.Email = fmt.Sprintf("Email address must be between 1 and %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.FirstName == "" || len(user.FirstName) > varcharMaxLength {

		user.Errors.FirstName = fmt.Sprintf("First name must be between 1 and %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.LastName == "" || len(user.LastName) > varcharMaxLength {

		user.Errors.LastName = fmt.Sprintf("Last name must be between 1 and %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.DisplayName != "" && len(user.DisplayName) > varcharMaxLength {

		user.Errors.LastName = fmt.Sprintf("Display name must no more than %d characters", varcharMaxLength)
		user.valid = false

	}

}
