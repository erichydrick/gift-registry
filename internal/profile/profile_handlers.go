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
	Household    string
	LastName     string
}

type userData struct {
	DisplayName   string
	Errors        profileErrors
	Email         string
	FirstName     string
	HouseholdName string
	LastName      string
	householdID   int64
	personID      int64
	valid         bool
}

const (
	lookupPersonQuery = `
		SELECT p.person_id, 
			h.household_id,
			p.email, 
			p.first_name, 
			p.last_name, 
			p.display_name, 
			h.name
		FROM person p
			INNER JOIN household_person hp ON p.person_id = hp.person_id
			INNER JOIN household h ON hp.household_id = h.household_id
		WHERE p.person_id = $1
	`
	updatePersonQuery = `
		UPDATE person SET email = $1, first_name = $2, last_name = $3, display_name = $4 
		WHERE person_id = $5
	`
	updateHouseholdQuery = `
		UPDATE household AS h  
		SET name = $1	
		FROM household_person AS hp
			JOIN person AS p ON hp.person_id = p.person_id
		WHERE hp.household_id = h.household_id
			AND p.person_id = $2
	`
	varcharMaxLength = 255
)

// Looks up the person information and returns it.
func ProfileHandler(svr *util.ServerUtils) http.HandlerFunc {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		var user userData
		personID := middleware.PersonID(res, req)
		svr.DB.QueryRow(ctx, lookupPersonQuery, personID).
			Scan(
				&user.personID,
				&user.householdID,
				&user.Email,
				&user.FirstName,
				&user.LastName,
				&user.DisplayName,
				&user.HouseholdName,
			)

		/*
			By default we display people by first name, but that can be overridden in
			the database with something like a "grandparent name"
		*/
		if user.DisplayName == "" {
			user.DisplayName = user.FirstName
		}

		attributes := middleware.TelemetryAttributes(ctx)
		attributes = append(attributes, attribute.Int64("personID", user.personID))
		attributes = append(attributes, attribute.Int64("householdID", user.householdID))
		attributes = append(attributes, attribute.String("firstName", user.FirstName))
		attributes = append(attributes, attribute.String("lastName", user.LastName))
		attributes = append(attributes, attribute.String("displayName", user.DisplayName))
		attributes = append(attributes, attribute.String("householdName", user.HouseholdName))
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
		svr.Logger.DebugContext(
			ctx,
			"Found the person ID from the session",
			slog.Int64("personID", personID),
		)

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
			DisplayName:   req.FormValue("displayName"),
			Email:         req.FormValue("email"),
			FirstName:     req.FormValue("firstName"),
			HouseholdName: req.FormValue("householdName"),
			LastName:      req.FormValue("lastName"),
		}
		svr.Logger.DebugContext(
			ctx,
			"Received a profile update request",
			slog.Any("submittedData", user),
		)

		attributes = append(attributes, attribute.Int64("personID", personID))
		attributes = append(attributes, attribute.String("updatedDisplayName", user.DisplayName))
		attributes = append(attributes, attribute.String("updatedEmail", user.Email))
		attributes = append(attributes, attribute.String("updatedFirstName", user.FirstName))
		attributes = append(attributes, attribute.String("updatedHouseholdName", user.HouseholdName))
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
		svr.Logger.DebugContext(
			ctx,
			"Validated submitted user data",
			slog.Any("userData", user),
		)
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

		sqlStatements := []string{updatePersonQuery, updateHouseholdQuery}
		sqlParams := [][]any{{user.Email, user.FirstName, user.LastName, user.DisplayName, personID}, {user.HouseholdName, personID}}
		_, errs := svr.DB.ExecuteBatch(ctx, sqlStatements, sqlParams)
		for _, err := range errs {
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
		}

		/* On save, return the form with the updated database values */
		var otherUser userData
		svr.DB.QueryRow(ctx, lookupPersonQuery, personID).Scan(&otherUser.personID, &otherUser.householdID, &otherUser.Email, &otherUser.FirstName, &otherUser.LastName, &otherUser.DisplayName, &otherUser.HouseholdName)
		svr.Logger.DebugContext(
			ctx,
			"Looked up the user profile data from the database",
			slog.Any("user", user),
			slog.Any("otherUser", otherUser),
		)
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

	if user.Email == "" {

		user.Errors.Email = "Email address is required"
		user.valid = false

	} else if len(user.Email) > varcharMaxLength {

		user.Errors.Email = fmt.Sprintf("Email address can't be more than %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.FirstName == "" {

		user.Errors.FirstName = "First name is required"
		user.valid = false

	} else if len(user.FirstName) > varcharMaxLength {

		user.Errors.FirstName = fmt.Sprintf("First name can't be more than %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.LastName == "" {

		user.Errors.LastName = "Last name is required"
		user.valid = false

	} else if len(user.LastName) > varcharMaxLength {

		user.Errors.LastName = fmt.Sprintf("Last name can't be more than %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.DisplayName != "" && len(user.DisplayName) > varcharMaxLength {

		user.Errors.LastName = fmt.Sprintf("Display name must no more than %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.HouseholdName == "" {

		user.Errors.Household = "Household name is required"
		user.valid = false

	} else if len(user.HouseholdName) > varcharMaxLength {

		user.Errors.Household = fmt.Sprintf("Household name cannot be more than %d characters", varcharMaxLength)
		user.valid = false

	}

}
