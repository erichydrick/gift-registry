package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"gift-registry/internal/middleware"
	"gift-registry/internal/templates"
	"gift-registry/internal/util"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	deleteSessionStatement = `DELETE
		FROM sessions
		WHERE session_id = ?`
	deleteSessionForPersonStatement = `DELETE
		FROM sessions
		WHERE person_id = ?`
	deleteVerificationTokenStatement = `DELETE 
		FROM verifications 
		WHERE person_id = ?`
	getVerificationQuery = `SELECT v.person_id, v.token, v.token_expiration, v.attempts 
		FROM verifications v 
			INNER JOIN people p ON p.person_id = v.person_id 
		WHERE p.email = ?`
	insertSessionStatement = `INSERT INTO sessions (session_id, person_id, expiration, user_agent) 
		VALUES (?, ?, ?, ?)`
	loginFailed            = "Login process failed. Please try again"
	maxAttempts            = 3
	selectUserByEmailQuery = `SELECT person_id, email 
		FROM people 
		WHERE email = ?`
	setVerificationTokenStatement = `INSERT INTO verifications (token, token_expiration, person_id) 
		VALUES (?, ?, ?) 
		ON CONFLICT (person_id) DO 
			UPDATE SET token = ?, token_expiration = ?
			WHERE person_id = ?`
	updateAttemptCountStatement = `UPDATE verifications 
		SET attempts = ? 
		WHERE person_id = ?`
)

// LoginHandler starts the login process by checking the provided email address
// against the person table. If there's an account associated with that email,
// triggers an email with a verification token to complete the login process.
func LoginHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("login_handler")

		userData := loginForm{
			success: true,
		}

		/* Don't bother doing anything on bad user data */
		err := req.ParseForm()
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error parsing the form data!",
				slog.String("errorMessage", errorMessage),
			)
			userData.Errors.ErrorMessage = "Error parsing the form data"
			userData.success = false
			tmplErr := templates.WriteResponseTemplate(
				ctx,
				res,
				400,
				"login-form",
				[]string{"login_form.html"},
				userData,
			)
			if tmplErr != nil {
				errorMessage = fmt.Sprintf("%v: %v", tmplErr, err)
			}
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}

		svr.Logger.DebugContext(
			ctx,
			"Processing login",
			slog.Any("body", req.Form),
			slog.String("url", req.URL.String()),
		)
		userData.Email = req.PostFormValue("email")

		userData.validate(ctx, svr)
		svr.Logger.DebugContext(
			ctx,
			"Ran validation on user login submission",
			slog.Any("validationErrors", userData.Errors),
		)

		/*
			Send the user details back to leave the form populated, but add error
			messaging (also capture the associated telemetry)
		*/
		if userData.Errors.Email != "" {

			userData.success = false
			_ = templates.WriteResponseTemplate(
				ctx,
				res,
				400,
				"login-form",
				[]string{"login_form.html"},
				userData,
			)
			return

		}

		var personID int64 = 0

		if err := svr.DB.QueryRow(ctx, selectUserByEmailQuery, userData.Email).Scan(&personID, &userData.Email); err != nil && err != sql.ErrNoRows {

			svr.Logger.ErrorContext(
				ctx,
				"Could not read person from the database",
				slog.String("errorMessage", err.Error()),
				slog.String("userEmail", userData.Email),
			)

			userData.Errors.ErrorMessage = "We had an error trying to log you on, we're looking into it now."

			_ = templates.WriteResponseTemplate(
				ctx,
				res,
				500,
				"login-form",
				[]string{"login_form.html"},
				userData,
			)
			return

		}

		var modified int64 = 0
		token := ""

		modified, token, err = setVerificationCode(ctx, svr, personID, &userData)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error creating a verification token",
				slog.String("errorMessage", err.Error()),
				slog.Int64("personID", personID),
			)
			tmplErr := templates.WriteResponseTemplate(
				ctx,
				res,
				500,
				"login-form",
				[]string{"login_form.html"},
				userData,
			)
			if tmplErr != nil {
				errorMessage = fmt.Sprintf("%v: %v", tmplErr, err)
			}
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}

		if modified == 1 {

			/*
				Do this in a virtual thread so we can return immediately.
			*/
			go sendVerificationEmail(
				ctx,
				userData.Email,
				token,
				svr.Logger,
				svr.Getenv,
			)

		}

		/* Capture if the login attempt matched a user in the database */
		span.SetAttributes(
			attribute.Int64("person_id", personID),
			attribute.Bool("email_found", modified == 1),
		)

		userVerify := verificationForm{
			Email: userData.Email,
		}
		svr.Logger.DebugContext(
			ctx,
			"Moving user to the verification form",
			slog.String("emailEntered", userData.Email),
		)
		err = templates.WriteResponseTemplate(
			ctx,
			res,
			200,
			"verify-login-form",
			[]string{"verify_login.html"},
			userVerify,
		)
		if err != nil {
			span.SetAttributes(attribute.String("error_message", err.Error()))
		}
	})
}

func LoginFormHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("login_form_handler")

		err := templates.WriteResponsePage(
			ctx,
			res,
			200,
			"login-page",
			[]string{
				"login_page.html",
				"login_form.html",
			},
			loginForm{},
		)
		if err != nil {
			span.SetAttributes(attribute.String("error_message", err.Error()))
		}
	})
}

// Logs the currently logged in user out of their current session. Clears the
// session cookie from the response. If there is no active session for the
// user, the logout method just returns.
func LogoutHandler(svr *util.ServerUtils) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		/* Clear the session cookie */
		cookie, err := req.Cookie(middleware.SessionCookie)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error looking up session cookie",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			return
		}
		cookie.Expires = time.Unix(0, 0)
		cookie.MaxAge = -1

		if _, err := svr.DB.Execute(ctx, deleteSessionStatement, cookie.Value); err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error cleaning up the session table",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			return
		}

		http.Redirect(res, req, "/login", http.StatusSeeOther)
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
		span := trace.SpanFromContext(ctx)
		span.SetName("verification_handler")

		submission := verificationForm{
			success: true,
		}

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error parsing the form data!", slog.String("errorMessage", err.Error()))
			submission.Errors.ErrorMessage = "Error parsing the form data"
			submission.success = false
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		submission.Code = req.FormValue("code")
		submission.Email = req.FormValue("email")
		submission.validate(ctx, svr)
		if !submission.success {
			err := templates.WriteResponseTemplate(
				ctx,
				res,
				200,
				"verify-login-form",
				[]string{"verify_login_form.html"},
				submission,
			)
			if err != nil {
				span.SetAttributes(attribute.String("templateError", err.Error()))
			}
			return
		}

		/* Look up the verification record */
		recData := verificationRecord{}
		err = svr.DB.QueryRow(ctx, getVerificationQuery, submission.Email).
			Scan(&recData.personID, &recData.token, &recData.tokenExpires, &recData.attempts)

		/*
			Handle errors looking up verification details (other than not finding the
			record
		*/
		if err != nil {
			if err == sql.ErrNoRows {
				errorMessage := fmt.Sprintf("could not look up verification record: %v", err)
				svr.Logger.InfoContext(
					ctx,
					"Could not find verification record",
					slog.String("userEmail", submission.Email),
				)
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"login-form",
					[]string{"login_form.html"},
					loginWithError(loginFailed),
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))
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
				errorMessage :=
					fmt.Sprintf(
						"error looking up verification code for %v: %v",
						submission.Email,
						err,
					)
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"login-form",
					[]string{"login_form.html"},
					submission,
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, err)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))
				return
			}
			_ = templates.WriteResponseTemplate(
				ctx,
				res,
				200,
				"login-form",
				[]string{"login_form.html"},
				loginWithError(loginFailed),
			)
			return

		} else {
			svr.Logger.DebugContext(ctx, "Read in record data", slog.String("userEmail", submission.Email))
		}

		codesMatch, attemptsRemaining, beforeExpiration := compareValidation(recData, submission)
		span.SetAttributes(
			attribute.Bool("codes_match", codesMatch),
			attribute.Bool("attempts_remaining", attemptsRemaining),
			attribute.Bool("before_expiration", beforeExpiration),
		)
		svr.Logger.DebugContext(ctx,
			"Checked verification fields",
			slog.String("tokenProvided", submission.Code),
			slog.Bool("codesMatch", codesMatch),
			slog.Bool("attemptsRemaining", attemptsRemaining),
			slog.Bool("beforeExpiration", beforeExpiration),
		)

		switch {

		case codesMatch && !beforeExpiration:

			svr.Logger.DebugContext(
				ctx,
				"EXPIRED",
				slog.String("email", submission.Email),
			)
			err = deleteVerification(ctx, svr, recData.personID)
			if err != nil {
				errorMessage := fmt.Sprintf("could not clean up expired verification code: %v", err)
				svr.Logger.ErrorContext(
					ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", errorMessage),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"verify-login-form",
					[]string{"verify_login.html"},
					submission,
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))
				return
			}
			span.SetAttributes(attribute.String("error_message", "verification code expired"))
			_ = templates.WriteResponseTemplate(
				ctx,
				res,
				200,
				"login-form",
				[]string{"login_form.html"},
				loginWithError(loginFailed),
			)
			return

		case !codesMatch:

			if attemptsRemaining {

				submission.success = false
				submission.Errors.ErrorMessage = "There was a problem confirming your verification code, please re-enter the code and try again"
				updateAttemptCount(ctx, svr, submission.Email, recData.personID, recData.attempts)
				errorMessage := "codes don't match"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"verify-login-form",
					[]string{"verify_login_form.html"},
					submission,
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))

			} else {

				err := deleteVerification(ctx, svr, recData.personID)
				if err != nil {
					errorMessage := fmt.Sprintf("could not delete failed verification: %v", err)
					svr.Logger.ErrorContext(ctx,
						"Error cleaning up the verification table!",
						slog.String("userEmail", submission.Email),
						slog.String("errorMessage", err.Error()),
					)
					submission.success = false
					submission.Errors.ErrorMessage = "Error completing login, please try again"
					tmplErr := templates.WriteResponseTemplate(
						ctx,
						res,
						200,
						"login-form",
						[]string{"login_form.html"},
						submission,
					)
					if tmplErr != nil {
						errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
					}
					span.SetAttributes(attribute.String("error_message", errorMessage))
					return
				}

				errorMessage := "codes don't match and the user has no more attempts"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"login-form",
					[]string{"login_form_html"},
					loginWithError(loginFailed),
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))

			}

		default:
			/*
				Clean up the verification record so this code can't be re-used
			*/
			err = deleteVerification(ctx, svr, recData.personID)
			if err != nil {
				errorMessage := fmt.Sprintf("could not clean up verification token: %v", err)
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"verify-login-form",
					[]string{"verify_login.html"},
					submission,
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))
				/*
					Not returning early because we don't want to stop the otherwise valid
					login
				*/
			}

			sessionID, sessionExpires, err := createSession(ctx, svr, req, recData.personID, submission.Email)
			if err != nil {
				errorMessage := fmt.Sprintf("could not create sessions: %v", err)
				svr.Logger.ErrorContext(
					ctx,
					"Error writing a session record!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				tmplErr := templates.WriteResponseTemplate(
					ctx,
					res,
					200,
					"verify-login-form",
					[]string{"verify_login.html"},
					submission,
				)
				if tmplErr != nil {
					errorMessage = fmt.Sprintf("%v: %v", tmplErr, errorMessage)
				}
				span.SetAttributes(attribute.String("error_message", errorMessage))
				return
			}

			submission.success = true

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
			span.SetAttributes(attribute.Bool("submission_success", submission.success))

		}
	})
}

func createSession(
	ctx context.Context,
	svr *util.ServerUtils,
	req *http.Request,
	personID int64,
	email string,
) (string, time.Time, error) {
	svr.Logger.InfoContext(ctx,
		"Starting a new authenticated session",
		slog.String("userEmail", email),
	)

	expires := time.Now().Add(5 * time.Minute).UTC()
	sessionID := rand.Text()
	userAgent := req.UserAgent()

	/*
		I'm not going to return this error, since it's not a requirement to being
		able to create a new session, but I do want to capture it just in case.
	*/
	if _, err := svr.DB.Execute(ctx, deleteSessionForPersonStatement, personID); err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error cleaning up old sessions",
			slog.String("errorMessage", err.Error()),
		)
	}

	res, err := svr.DB.Execute(ctx, insertSessionStatement, sessionID, personID, expires, userAgent)
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
	res, err := svr.DB.Execute(ctx, deleteVerificationTokenStatement, personID)
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
		updateAttemptCountStatement,
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
	userData *loginForm,
) (int64, string, error) {

	token := rand.Text()
	expires := time.Now().Add(5 * time.Minute).UTC()
	svr.Logger.DebugContext(
		ctx,
		"Created a login token",
		slog.String("userEmail", userData.Email),
		slog.Int64("personID", personID),
	)

	/*
		personID is less than or equal to 0 if the email isn't in the database.
		That's not an error per se, but they don't get a verification token either.
	*/
	if personID <= 0 {

		return 0, "", nil

	}

	rows, err := svr.DB.Execute(
		ctx,
		setVerificationTokenStatement,
		token,
		expires,
		personID,
		token,
		expires,
		personID,
	)
	if err != nil {
		switch {

		/*
			This happens when an email not in the DB is submitted. Swallow it so we
			don't given away valid emails to a scan
		*/
		case strings.Contains(err.Error(), "violates foreign key constraint \"verification_person_id_fkey\""):
			svr.Logger.WarnContext(
				ctx,
				"Person not found in database, returning normally with no code set",
				slog.String("userEmail", userData.Email),
				slog.Int64("personID", personID),
				slog.String("errorMessage", err.Error()),
			)
			return 0, "", nil

		default:
			svr.Logger.ErrorContext(
				ctx,
				"Error saving token information!",
				slog.String("errorMessage", err.Error()),
			)
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
		svr.Logger.ErrorContext(
			ctx,
			"Error getting the number of rows modified when saving a token",
			slog.String("errorMessage", err.Error()),
		)
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
	attemptsRemaining = maxAttempts-(record.attempts+1) > 0

	return
}

func (lf *loginForm) validate(ctx context.Context, svr *util.ServerUtils) {

	svr.Logger.DebugContext(
		ctx,
		"Validating the login form",
		slog.String("serverForm", lf.String()),
	)

	if _, err := mail.ParseAddress(strings.Trim(lf.Email, " ")); err != nil {
		lf.Errors.Email = "Invalid email address"
	}

}

func (lf loginForm) emailAddress() string {
	return lf.Email
}

func (lf loginForm) String() string {
	return fmt.Sprintf("email=%s, validated=%v, errors={%v}", lf.Email, lf.success, lf.Errors.String())
}

func (lfe loginFormErrors) String() string {
	return fmt.Sprintf("email=%v, errors=%v", lfe.Email, lfe.ErrorMessage)
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

func sendVerificationEmail(
	ctx context.Context,
	userEmail string,
	token string,
	logger *slog.Logger,
	getenv func(string) string,
) {

	logger.DebugContext(ctx, "Sending user email with the login token", slog.String("userEmail", userEmail), slog.Any("emailer", emailer))

	emailErr := emailer.SendVerificationEmail(ctx, []string{userEmail}, token, getenv)
	if emailErr != nil {
		logger.ErrorContext(
			ctx,
			"Error sending verification email",
			slog.String("errorMessage", emailErr.Error()),
		)
	}

}
