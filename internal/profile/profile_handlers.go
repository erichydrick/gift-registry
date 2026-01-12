// Package profile handles all the user profile interations, from updating
// names and emails as well as viewing the profiles for people being managed
// by household members (like small children).
package profile

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"gift-registry/internal/middleware"
	"gift-registry/internal/util"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	ExternalID    string
	FirstName     string
	HouseholdName string
	LastName      string
	Type          string
	householdID   int64
	personID      int64
	valid         bool
}

type pageData struct {
	DisplayName string
	LastName    string
	Profiles    []userData
}

const (
	/*
		The second part of the WHERE clause here ensures that the external ID either
		belongs to the logged in user or an account that user manages.
	*/
	externalIDLookupQuery = `SELECT p.person_id, 
			p.external_id,
			p.type
		FROM person p
			INNER JOIN household_person hp on hp.person_id = p.person_id
		WHERE p.external_id = $1
			AND (hp.person_id = $2 OR (p.type = 'MANAGED' AND hp.household_id = (SELECT household_id FROM household_person WHERE person_id = $3)))`
	lookupManagedProfilesQuery = `SELECT p.person_id, 
			h.household_id,
			p.external_id,
			p.first_name, 
			p.last_name, 
			p.display_name, 
			p.type,
			h.name
		FROM person p
			INNER JOIN household_person hp ON p.person_id = hp.person_id
			INNER JOIN household h ON hp.household_id = h.household_id
		WHERE h.household_id = $1
			AND p.type = 'MANAGED'`
	lookupPersonQuery = `SELECT p.person_id, 
			h.household_id,
			p.external_id,
			p.email, 
			p.first_name, 
			p.last_name, 
			p.display_name, 
			p.type,
			h.name
		FROM person p
			INNER JOIN household_person hp ON p.person_id = hp.person_id
			INNER JOIN household h ON hp.household_id = h.household_id
		WHERE p.person_id = $1`
	updatePersonQuery = `UPDATE person SET email = $1, first_name = $2, last_name = $3, display_name = $4 
		WHERE external_id = $5`
	updateHouseholdQuery = `UPDATE household AS h  
		SET name = $1	
		FROM household_person AS hp
			JOIN person AS p ON hp.person_id = p.person_id
		WHERE hp.household_id = h.household_id
			AND p.person_id = $2`
	varcharMaxLength = 255
)

// ProfileHandler looks up the person information and returns it, along with
// any other managed profiles in the household.
func ProfileHandler(svr *util.ServerUtils) http.HandlerFunc {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("profile_handler")

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
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		profile := pageData{
			Profiles: []userData{},
		}

		var person userData
		personID := middleware.PersonID(res, req)
		profileIDs := []int64{personID}
		span.SetAttributes(attribute.Int64("person_id", personID))
		err = svr.DB.QueryRow(ctx, lookupPersonQuery, personID).
			Scan(
				&person.personID,
				&person.householdID,
				&person.ExternalID,
				&person.Email,
				&person.FirstName,
				&person.LastName,
				&person.DisplayName,
				&person.Type,
				&person.HouseholdName,
			)
		if err != nil {
			person = userData{
				Errors: profileErrors{
					ErrorMessage: "Could not look up profile information.",
				},
			}
			res.WriteHeader(500)
			err = tmpl.ExecuteTemplate(res, "profile-page", profile)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error writing template!",
					slog.String("errorMessage", err.Error()),
				)
				res.WriteHeader(500)
				res.Write([]byte("Error loading your profile page"))
				span.SetAttributes(attribute.String("error_message", err.Error()))
				return
			}
		}

		/*
			By default we display people by first name, but that can be overridden in
			the database with something like a "grandparent name"
		*/
		if person.DisplayName == "" {
			person.DisplayName = person.FirstName
		}

		/*
			The calling user's profile should always be first, and be form the page
			title
		*/
		if person.Type != "MANAGED" &&
			(profile.DisplayName == "" || profile.LastName == "") {

			profile.DisplayName = person.DisplayName
			profile.LastName = person.LastName

		}

		profile.Profiles = append(profile.Profiles, person)

		/*
			Append any managed profiles to response so the logged-in user can manage
			dependent profiles as well.
		*/
		rows, err := svr.DB.Query(ctx, lookupManagedProfilesQuery, person.householdID)
		if err != nil {
			/*
				This is technically an error (because querying failed), and we should show
				it to the user, but we should still return normally because we have the
				user's profile at least
			*/
			profile.Profiles[0].Errors.ErrorMessage = "Could not look up associated managed profiles."
		}

		for rows.Next() {

			err = rows.Scan(
				&person.personID,
				&person.householdID,
				&person.ExternalID,
				&person.FirstName,
				&person.LastName,
				&person.DisplayName,
				&person.Type,
				&person.HouseholdName,
			)
			if err != nil {
				svr.Logger.ErrorContext(ctx, "Error scanning data!", slog.String("errorMessage", err.Error()))
				continue
			}

			profile.Profiles = append(profile.Profiles, person)
			profileIDs = append(profileIDs, person.personID)

		}

		span.SetAttributes(attribute.String("profiles_returned", fmt.Sprintf("%v", profileIDs)))

		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "profile-page", profile)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading your profile page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

	})

}

// Updates the person's information with the values provided from form input.
func ProfileUpdateHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("profile_update")

		personID := middleware.PersonID(res, req)
		externalID := req.PathValue("externalID")

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error parsing the profile update form",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(400)
			res.Write([]byte("Could not user data"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		user := userData{
			DisplayName:   req.FormValue("displayName"),
			Email:         req.FormValue("email"),
			ExternalID:    req.FormValue("externalID"),
			FirstName:     req.FormValue("firstName"),
			HouseholdName: req.FormValue("householdName"),
			LastName:      req.FormValue("lastName"),
		}

		/*
			I'm capturing more than I normally would here, but if I need to debug an update failure, I will want to know what the values were.
		*/
		span.SetAttributes(
			attribute.Int64("person_id", personID),
			attribute.String("updated_external_id", externalID),
			attribute.String("updated_type", user.Type),
			attribute.String("updated_display_name", user.DisplayName),
			attribute.String("updated_email", user.Email),
			attribute.String("updated_first_name", user.FirstName),
			attribute.String("updated_household_name", user.HouseholdName),
			attribute.String("updated_last_name", user.LastName),
		)

		tmpl, err := template.ParseFiles(svr.Getenv("TEMPLATES_DIR") + "/profile_form.html")
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading the profile page template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading the profile page template!"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		err = svr.DB.QueryRow(ctx, externalIDLookupQuery, externalID, personID, personID).
			Scan(
				&user.personID,
				&user.ExternalID,
				&user.Type,
			)

		/* We can't validate the profile details, so we can't do an update */
		if err != nil {
			svr.Logger.ErrorContext(ctx,
				"Error looking up profile to update",
				slog.String("errorMessage", err.Error()),
			)
			user.Errors.ErrorMessage = "Could not update profile"
			err = tmpl.ExecuteTemplate(res, "profile-form", user)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error returning error to user",
					slog.String("errorMessage", err.Error()),
				)
				res.WriteHeader(500)
				res.Write([]byte("Error saving profile information"))
				span.SetAttributes(attribute.String("error_message", err.Error()))
				return
			}

		}
		span.SetAttributes(attribute.Int64("updated_person_id", user.personID))
		span.SetAttributes(attribute.String("updated_external_id", user.ExternalID))
		span.SetAttributes(attribute.String("updated_type", user.Type))

		/*
			We should always have a display name, so when in doubt use first name
		*/
		if user.DisplayName == "" {
			user.DisplayName = user.FirstName
		}

		user.validate()
		span.SetAttributes(attribute.Bool("data_valid", user.valid))

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
				span.SetAttributes(attribute.String("error_message", err.Error()))
			}

			return
		}

		sqlStatements := []string{updatePersonQuery}
		sqlParams := [][]any{{user.Email, user.FirstName, user.LastName, user.DisplayName, externalID}}

		/*
			TODO:
			THIS BEGS THE QUESTION OF IF UPDATING THE HOUSEHOLD NAME SHOULD BE A
			SEPARATE ACTION HITTING A SEPARATE ENDPOINT
		*/
		/*
			If the profile being updated isn't a managed profile (e.g. a child),
			there's a chance they may have edited the househole name, so we need to
			persist those changes too.
		*/
		if user.Type != "MANAGED" {

			sqlStatements = append(sqlStatements, updateHouseholdQuery)
			sqlParams = append(sqlParams, []any{user.HouseholdName, personID})

		}

		_, errs := svr.DB.ExecuteBatch(ctx, sqlStatements, sqlParams)
		for _, err := range errs {
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error updating the profile information",
					slog.String("errorMessage", err.Error()),
				)

				user.Errors.ErrorMessage = "Could not save the profile update"
				err = tmpl.ExecuteTemplate(res, "profile-form", user)
				if err != nil {
					svr.Logger.ErrorContext(
						ctx,
						"Error writing the profile page error messages",
						slog.String("errorMessage", err.Error()),
					)
					res.WriteHeader(500)
					res.Write([]byte("Error loading your profile page"))
					span.SetAttributes(attribute.String("error_message", err.Error()))
					return
				}
			}
		}

		err = tmpl.ExecuteTemplate(res, "profile-form", user)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error reading updated values into response",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading your profile page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

	})

}

func (user *userData) validate() {
	user.valid = true

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

	/* The below fields aren't part of the profile cards for managed profiles */
	if user.Email == "" && user.Type != "MANAGED" {

		user.Errors.Email = "Email address is required for non-managed person accounts"
		user.valid = false

	} else if len(user.Email) > varcharMaxLength {

		user.Errors.Email = fmt.Sprintf("Email address can't be more than %d characters", varcharMaxLength)
		user.valid = false

	}

	if user.HouseholdName == "" && user.Type != "MANAGED" {

		user.Errors.Household = "Household name is required"
		user.valid = false

	} else if len(user.HouseholdName) > varcharMaxLength {

		user.Errors.Household = fmt.Sprintf("Household name cannot be more than %d characters", varcharMaxLength)
		user.valid = false

	}
}

func (user userData) String() string {

	errors := "{}"
	if user.Errors.ErrorMessage != "" ||
		user.Errors.FirstName != "" ||
		user.Errors.LastName != "" ||
		user.Errors.Email != "" ||
		user.Errors.Household != "" {

		errors = fmt.Sprintf(
			"{ErrorMessage: %s, FirstName: %s, LastName: %s, Email: %s, Household: %s}",
			user.Errors.ErrorMessage,
			user.Errors.FirstName,
			user.Errors.LastName,
			user.Errors.Email,
			user.Errors.Household,
		)

	}

	return fmt.Sprintf(
		"{DisplayName: %s, Email: %s, ExternalID: %s, FirstName: %s, LastName: %s, Type: %s, HouseholdName: %s, Errors: %s}",
		user.DisplayName,
		user.Email,
		user.ExternalID,
		user.FirstName,
		user.LastName,
		user.Type,
		user.HouseholdName,
		errors,
	)
}
