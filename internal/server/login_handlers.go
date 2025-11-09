package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"gift-registry/internal/middleware"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

type Submitter interface {
	error
	fmt.Stringer
	emailAddress() string
	succeeded() bool
}

type loginForm struct {
	Email   string
	Errors  loginFormErrors
	success bool
}

type loginFormErrors struct {
	Email        string
	ErrorMessage string
}

type verificationForm struct {
	Code    string
	Email   string
	Errors  verificationFormErrors
	success bool
}

type verificationFormErrors struct {
	Code         string
	ErrorMessage string
}

type verificationRecord struct {
	attempts     int
	personID     int64
	token        string
	tokenExpires time.Time
}

const (
	DeleteVerificationTokenStatement = `DELETE 
		FROM verification 
		WHERE person_id = $1`
	GetVerificationQuery = `SELECT v.person_id, v.token, v.token_expiration, v.attempts 
		FROM verification v 
			INNER JOIN person p ON p.person_id = v.person_id 
		WHERE p.email = $1`
	InsertSessionStatement = `INSERT INTO session(session_id, person_id, expiration, user_agent) 
		VALUES ($1, $2, $3, $4)`
	LoginFailed            = "Login process failed. Please try again"
	MaxAttempts            = 3
	SelectUserByEmailQuery = `SELECT person_id, email 
		FROM person 
		WHERE email = $1`
	SetVerificationTokenStatement = `INSERT INTO verification (token, token_expiration, person_id) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (person_id) DO 
			UPDATE SET token = $1, token_expiration = $2`
	UpdateAttemptCountStatement = `UPDATE verification 
		SET attempts = $1 
		WHERE person_id = $2`
)

// Starts the login process by checking the provided email address against the
// person table. If there's an account associated with that email, triggers an
// email with a verification token to complete the login process.
func LoginHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		userData := loginForm{
			success: true,
		}

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error parsing the form data!", slog.String("errorMessage", err.Error()))
			userData.Errors.ErrorMessage = "Error parsing the form data"
			userData.success = false
			writeResponse(ctx, res, req, svr, userData, "/verify-login.html", "verify-login-form")
			return
		}

		svr.Logger.DebugContext(ctx, "Processing login", slog.Any("body", req.Form), slog.String("url", req.URL.String()))
		userData.Email = req.PostFormValue("email")

		userData.validate(ctx, svr)
		svr.Logger.DebugContext(ctx, "Ran validation on user login submission", slog.Any("validationErrors", userData.Errors))

		/*
			Send the user details back to leave the form populated, but add error
			messaging (also capture the associated telemetry)
		*/
		if userData.Errors.Email != "" {

			userData.success = false
			writeResponse(ctx, res, req, svr, userData, "/login-form.html", "login-form")
			return

		}

		var email string = ""
		var personID int64 = 0
		if err := svr.DB.QueryRow(ctx, SelectUserByEmailQuery, userData.Email).Scan(&personID, &email); err != nil && err != sql.ErrNoRows {
			svr.Logger.ErrorContext(ctx, "Could not read person from the database", slog.String("errorMessage", err.Error()), slog.String("userEmail", userData.Email))
		}

		var modified int64 = 0
		var token string = ""

		if email != "" {

			modified, token, err = setVerificationCode(ctx, svr, personID, &userData)
			if err != nil {
				writeResponse(ctx, res, req, svr, userData, "/login-form.html", "login-form")
				return
			}

		}

		var emailErr error = nil
		if modified == 1 {

			svr.Logger.DebugContext(ctx, "Sending user email with the login token", slog.String("userEmail", userData.Email))
			emailErr = emailer.SendVerificationEmail(ctx, []string{userData.Email}, token, svr.Getenv)

		}

		/* Capture if the login attempt matched a user in the database */
		attributes := middleware.TelemetryAttributes(ctx)
		attributes = append(attributes, attribute.Bool("emailFound", modified == 1))
		attributes = append(attributes, attribute.Bool("emailSuccess", emailErr == nil))
		ctx = middleware.WriteTelemetry(ctx, attributes)
		_ = req.WithContext(ctx)

		tmplPath := fmt.Sprintf("%s/%s", svr.Getenv("TEMPLATES_DIR"), "/verify-login.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading the login page template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading the login page template!"))
			return
		}

		userVerify := verificationForm{
			Email: userData.Email,
		}
		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "verify-login-form", userVerify)
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error writing template!",
				slog.String("errorMessage", err.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry login submission form"))
			return
		}

	})

}

func LoginFormHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		templates := svr.Getenv("TEMPLATES_DIR")
		svr.Logger.DebugContext(ctx, "Reading data from template directory", slog.String("templateDir", templates))
		tmpl, tmplErr := template.ParseFiles(templates+"/login-page.html", templates+"/login-form.html")

		if tmplErr != nil {
			svr.Logger.ErrorContext(ctx, "Error loading the login form template", slog.String("errorMessage", tmplErr.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry login"))
			return
		}

		res.WriteHeader(200)

		err := tmpl.ExecuteTemplate(res, "login-page", loginForm{})
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error writing template!",
				slog.String("errorMessage", err.Error()))
			res.WriteHeader(500)
			res.Write([]byte("Error loading gift registry login form"))
			return
		}

	})

}

// Verifies the given token is associated with the given email address in the
// database. If it is, start a new authenticated session and redirect to the
// registry page. If not, return an error. 3 failed login attempts and the
// token is removed from the database and the user is forced to re-enter their
// email address.
func VerificationHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		submission := verificationForm{
			success: true,
		}

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error parsing the form data!", slog.String("errorMessage", err.Error()))
			submission.Errors.ErrorMessage = "Error parsing the form data"
			submission.success = false
			return
		}

		submission.Code = req.FormValue("code")
		submission.Email = req.FormValue("email")
		submission.validate(ctx, svr)
		if !submission.success {
			writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")
		}

		/* Look up the verification record */
		recData := verificationRecord{}
		err = svr.DB.QueryRow(ctx, GetVerificationQuery, submission.Email).
			Scan(&recData.personID, &recData.token, &recData.tokenExpires, &recData.attempts)

		/*
			Handle errors looking up verification details (other than not finding the
			record
		*/
		if err != nil {

			if err == sql.ErrNoRows {
				svr.Logger.ErrorContext(ctx, "Could not find verification record", slog.String("userEmail", submission.Email))
				writeResponse(ctx, res, req, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")
				return
			}

			svr.Logger.ErrorContext(ctx,
				"Error looking up verification details from the database",
				slog.String("userEmail", submission.Email),
				slog.String("token", submission.Code),
				slog.String("errorMessage", err.Error()),
			)
			err = deleteVerification(ctx, svr, recData.personID)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, svr, submission, "/login-form.html", "login-form")
				return
			}
			writeResponse(ctx, res, req, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")
			return

		} else {
			svr.Logger.DebugContext(ctx, "Read in record data", slog.String("userEmail", submission.Email))
		}

		codesMatch, attemptsRemaining, beforeExpiration := compareValidation(recData, submission)
		svr.Logger.DebugContext(ctx,
			"Checked verification fields",
			slog.Bool("codesMatch", codesMatch),
			slog.Bool("attemptsRemaining", attemptsRemaining),
			slog.Bool("beforeExpiration", beforeExpiration),
		)
		switch {

		case codesMatch && !beforeExpiration:
			err = deleteVerification(ctx, svr, recData.personID)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")
			}
			writeResponse(ctx, res, req, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")

		case !codesMatch:
			if attemptsRemaining {

				submission.success = false
				submission.Errors.ErrorMessage = "There was a problem confirming your verification code, please re-enter the code and try again"
				updateAttemptCount(ctx, svr, submission.Email, recData.personID, recData.attempts)
				writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")

			} else {

				err := deleteVerification(ctx, svr, recData.personID)
				if err != nil {
					svr.Logger.ErrorContext(ctx,
						"Error cleaning up the verification table!",
						slog.String("userEmail", submission.Email),
						slog.String("errorMessage", err.Error()),
					)
					submission.success = false
					submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
					writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")
				}
				writeResponse(ctx, res, req, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")

			}

		default:
			/*
				Clean up the verification record so this code can't be re-used
			*/
			err = deleteVerification(ctx, svr, recData.personID)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")
			}

			sessionID, sessionExpires, err := createSession(ctx, svr, req, recData.personID, submission.Email)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error writing a session record!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, svr, submission, "/verify-login.html", "verify-login-form")
			}

			cookie := http.Cookie{
				Name:     middleware.SessionCookie,
				Value:    sessionID,
				MaxAge:   int(time.Until(sessionExpires).Seconds()),
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
			}
			http.SetCookie(res, &cookie)
			res.Header().Add("HX-Redirect", "/registry")

		}

	})

}

func createSession(
	ctx context.Context,
	svr *util.ServerUtils,
	req *http.Request,
	personID int64,
	email string) (string, time.Time, error) {

	svr.Logger.InfoContext(ctx,
		"Starting a new authenticated session",
		slog.String("userEmail", email),
	)

	expires := time.Now().Add(5 * time.Minute).UTC()
	sessionID := rand.Text()
	userAgent := req.UserAgent()

	res, err := svr.DB.Execute(ctx, InsertSessionStatement, sessionID, personID, expires, userAgent)
	if err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error inserting session record",
			slog.String("userEmail", email),
			slog.String("userAgent", userAgent),
			slog.String("errorMessage", err.Error()),
		)
		return "", time.Now(), fmt.Errorf("error saving session record to the database: %v", err)
	}

	/* Capture the number of rows modified, it should be 1 */
	if modified, err := res.RowsAffected(); err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error getting the number of rows modified saving the session",
			slog.String("userEmail", email),
			slog.String("userAgent", userAgent),
			slog.String("errorMessage", err.Error()),
		)
		/* Not returning an error since the database update itself worked. */
	} else if modified != 1 {
		/*
			In theory, the only non-1 value would be 0 since this was an INSERT
			operation. That said, checking modified != 1 leaves me coverage in
			case I was WILDLY off and we somehow write 2 session records (which would
			also be error-level bad)
		*/
		svr.Logger.ErrorContext(ctx,
			"Session data not actually updated",
			slog.Int64("rowsModified", modified),
			slog.String("userEmail", email),
			slog.String("userAgent", userAgent),
		)
		return "", time.Now(), fmt.Errorf("no records modified in the database")
	} else {
		svr.Logger.DebugContext(ctx,
			"Wrote the session information to the database",
			slog.String("userEmail", email),
			slog.String("userAgent", userAgent),
			slog.Int64("rowsModified", modified),
		)
	}

	return sessionID, expires, nil

}

func deleteVerification(ctx context.Context, svr *util.ServerUtils, personID int64) error {

	/* Make sure we have a transaction so we can roll back if this doesn't work */
	res, err := svr.DB.Execute(ctx, DeleteVerificationTokenStatement, personID)
	if err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error deleting the verification record",
			slog.String("errorMessage", err.Error()),
		)
		return fmt.Errorf("error executing the cleanup of the verification record: %v", err)
	}

	if cnt, err := res.RowsAffected(); err != nil {
		svr.Logger.WarnContext(ctx,
			"Error getting the count of rows affected",
			slog.String("errorMessage", err.Error()),
		)
	} else {
		/*
			TODO: CHANGING THIS FROM EMAIL TO PERSONID RAISES A BIGGER QUESTION - DO I
			NEED TO BE CAPTURING THINGS LIKE USER EMAIL IN LOG MESSAGES, OR CAN I
			CAPTURE THAT ONCE AND THEN FOLLOW THE TRACE TO GET ALL ASSOCIATED LOGS
			WITH A PARTICULAR LOGIN OPERATION?
		*/
		svr.Logger.InfoContext(ctx,
			"Cleaned up the verification table",
			slog.Int64("count", cnt),
			slog.Int64("personID", personID),
		)
	}

	return nil

}

func updateAttemptCount(ctx context.Context, svr *util.ServerUtils, email string, personID int64, attempt int) error {

	attempt++

	res, err := svr.DB.Execute(ctx,
		UpdateAttemptCountStatement,
		attempt,
		personID,
	)
	if res == nil || err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error updating the attempt count in the database",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.String("errorMessage", err.Error()),
		)
	}

	return nil

}

func loginWithError(errorMessage string) loginForm {

	return loginForm{
		Errors: loginFormErrors{
			ErrorMessage: errorMessage,
		},
		success: false,
	}

}

func setVerificationCode(
	ctx context.Context,
	svr *util.ServerUtils,
	personID int64,
	userData *loginForm) (int64, string, error) {

	token := rand.Text()
	expires := time.Now().Add(5 * time.Minute).UTC()
	svr.Logger.DebugContext(ctx, "Created a login token", slog.String("userEmail", userData.Email))

	rows, err := svr.DB.Execute(ctx, SetVerificationTokenStatement, token, expires, personID)
	if err != nil {

		switch {

		/*
			This happens when an email not in the DB is submitted. Swallow it so we
			don't given away valid emails to a scan
		*/
		case strings.Contains(err.Error(), "violates foreign key constraint \"verification_person_id_fkey\""):
			svr.Logger.WarnContext(ctx,
				"Person not found in database, returning normally with no code set",
				slog.String("userEmail", userData.Email),
				slog.Int64("personID", personID),
				slog.String("errorMessage", err.Error()),
			)
			return 0, "", nil

		default:
			svr.Logger.ErrorContext(ctx, "Error saving token information!", slog.String("errorMessage", err.Error()))
			userData.success = false
			return 0, token, fmt.Errorf("could not write verification token information to the database: %v", err)

		}

	}

	svr.Logger.DebugContext(
		ctx,
		"Ran the token INSERT command",
		slog.String("userEmail", userData.Email),
		slog.Int64("personID", personID),
	)
	modified, err := rows.RowsAffected()
	/*
		Not returning this as an error because the main objective (create a
		verification token and store it in the database for user verification)
		succeeded. I do want a record of this in the logs though.
	*/
	if err != nil {
		svr.Logger.ErrorContext(ctx, "Error getting the number of rows modified when saving a token", slog.String("errorMessage", err.Error()))
	}

	return modified, token, nil

}

func compareValidation(record verificationRecord, submission verificationForm) (tokensMatch bool, attemptsRemaining bool, beforeExpiration bool) {

	now := time.Now().UTC()

	tokensMatch = strings.EqualFold(record.token, submission.Code)
	beforeExpiration = now.Before(record.tokenExpires)

	/*
		Adding 1 to the attempts count stored in the database
		to account for the attempt we're making right now
	*/
	attemptsRemaining = MaxAttempts-(record.attempts+1) > 0

	return

}

func writeResponse(ctx context.Context,
	res http.ResponseWriter,
	req *http.Request,
	svr *util.ServerUtils,
	submission Submitter,
	templateFile string,
	templateDef string,
) {

	attributes := middleware.TelemetryAttributes(ctx)

	attributes = append(attributes, attribute.Bool("successful", submission.succeeded()))
	attributes = append(attributes, attribute.String("email", submission.emailAddress()))
	ctx = middleware.WriteTelemetry(ctx, attributes)
	_ = req.WithContext(ctx)

	tmplPath := fmt.Sprintf("%s/%s", svr.Getenv("TEMPLATES_DIR"), templateFile)

	tmpl, tmplErr := template.ParseFiles(tmplPath)
	if tmplErr != nil {
		res.WriteHeader(500)
		res.Write([]byte("Error loading the login page template!"))
		return
	}

	res.WriteHeader(200)
	err := tmpl.ExecuteTemplate(res, templateDef, submission)
	if err != nil {
		svr.Logger.ErrorContext(ctx, "Error writing template!",
			slog.String("errorMessage", err.Error()))
		res.WriteHeader(500)
		res.Write([]byte("Error loading gift registry login form"))
		return
	}

}

func (lf *loginForm) validate(ctx context.Context, svr *util.ServerUtils) {

	svr.Logger.DebugContext(ctx, "Validating the login form", slog.String("serverForm", lf.String()))
	if _, err := mail.ParseAddress(strings.Trim(lf.Email, " ")); err != nil {

		lf.Errors.Email = "Invalid email address"

	}

	svr.Logger.DebugContext(ctx, "Form data is now", slog.String("serverForm", lf.String()))

}

func (lf loginForm) emailAddress() string {

	return lf.Email

}

func (lf loginForm) String() string {

	return fmt.Sprintf("email=%s, validated=%v, errors=%v", lf.Email, lf.success, lf.Errors)

}

func (lf loginForm) succeeded() bool {

	return lf.success

}

func (lf loginForm) Error() string {

	return fmt.Sprintf("formErrors=%s, emailErrors=%s", lf.Errors.ErrorMessage, lf.Errors.Email)

}

func (vf *verificationForm) validate(ctx context.Context, svr *util.ServerUtils) {

	svr.Logger.DebugContext(ctx, "Validating the verification form", slog.String("verificationForm", vf.String()))

	if vf.Code == "" {

		vf.Errors.Code = "Verification code is required."

	}

	svr.Logger.DebugContext(ctx, "Form data is now", slog.String("verificationForm", vf.String()))

}

func (vf verificationForm) emailAddress() string {

	return vf.Email

}

func (vf verificationForm) String() string {

	return fmt.Sprintf("email=%s, validated=%v, errors=%v", vf.Email, vf.success, vf.Errors)

}

func (vf verificationForm) succeeded() bool {

	return vf.success

}

func (vf verificationForm) Error() string {

	return fmt.Sprintf("formErrors=%s, codeErrors=%s", vf.Errors.ErrorMessage, vf.Errors.Code)

}
