package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	email        string
	token        string
	tokenExpires time.Time
}

const (
	LoginFailed   = "Login process failed. Please try again"
	MaxAttempts   = 3
	SessionCookie = "gift-registry-session"
)

var (
	loginCtr        metric.Int64Counter
	verificationCtr metric.Int64Counter
)

func init() {

	var err error

	loginCtr, err = meter.Int64Counter(
		"login.counter",
		metric.WithDescription("Number of login attempts"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		panic(err)
	}

	verificationCtr, err = meter.Int64Counter(
		"verification.counter",
		metric.WithDescription("Number of email verification attempts"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		panic(err)
	}

}

// Creates a new user account in the person table. The login is valid if the
// user has provided a properly formatted email address, a first name, and a
// last name (we're not tracking any other user details).
func LoginHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx, span := tracer.Start(req.Context(), "login")
		defer span.End()

		userData := loginForm{
			success: true,
		}

		err := req.ParseForm()
		if err != nil {
			svr.Logger.ErrorContext(ctx, "Error parsing the form data!", slog.String("errorMessage", err.Error()))
			userData.Errors.ErrorMessage = "Error parsing the form data"
			userData.success = false
			writeResponse(ctx, res, req, span, loginCtr, svr, userData, "/verify-login.html", "verify-login-form")
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
			writeResponse(ctx, res, req, span, loginCtr, svr, userData, "/login-form.html", "login-form")
			return

		}

		pplRows := svr.DB.QueryRowContext(ctx, "SELECT email FROM person WHERE email = $1", userData.Email)
		var email string = ""
		if err = pplRows.Scan(&email); err != nil && err != sql.ErrNoRows {
			svr.Logger.ErrorContext(ctx, "Could not read email from the database", slog.String("userEmail", userData.Email))
		}

		var modified int64 = 0
		var token string = ""

		if email != "" {

			modified, token, err = setVerificationCode(ctx, svr, &userData)
			if err != nil {
				writeResponse(ctx, res, req, span, loginCtr, svr, userData, "/login-form.html", "login-form")
				return
			}

		}

		var emailErr error = nil
		if modified == 1 {

			svr.Logger.DebugContext(ctx, "Sending user email with the login token", slog.String("userEmail", userData.Email))
			emailErr = emailer.SendVerificationEmail([]string{userData.Email}, token, svr.Getenv)
			if emailErr != nil {
				svr.Logger.ErrorContext(ctx,
					"Failed to send the verification email",
					slog.String("userEmail", userData.Email),
					slog.String("errorMessage", emailErr.Error()),
				)
			}

		}

		/* Capture if the login attempt matched a user in the database */
		span.SetAttributes(attribute.Bool("emailFound", modified == 1))
		span.SetAttributes(attribute.Bool("emailSuccess", emailErr == nil))

		tmplPath := fmt.Sprintf("%s/%s", svr.Getenv("TEMPLATES_DIR"), "/verify-login.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			res.WriteHeader(500)
			res.Write([]byte("Error loading the login page template!"))
			return
		}

		svr.Logger.InfoContext(ctx,
			fmt.Sprintf("Finished the operation %s", req.URL.Path),
			slog.String("userData", userData.String()),
		)

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

		ctx, span := tracer.Start(req.Context(), "loginForm")
		defer span.End()

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

		svr.Logger.InfoContext(ctx, fmt.Sprintf("Finished the operation %s",
			req.URL.Path))
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

		ctx, span := tracer.Start(req.Context(), "verification")
		defer span.End()

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
			writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
		}

		/* Look up the verification record */
		recData := verificationRecord{}
		err = svr.DB.QueryRowContext(ctx, "SELECT email, token, token_expiration, attempts FROM verification WHERE email = $1", submission.Email).Scan(&recData.email, &recData.token, &recData.tokenExpires, &recData.attempts)

		/*
			Handle errors looking up verification details (other than not finding the
			record
		*/
		if err != nil {

			if err == sql.ErrNoRows {
				svr.Logger.ErrorContext(ctx, "Could not find verification record", slog.String("userEmail", submission.Email))
				writeResponse(ctx, res, req, span, verificationCtr, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")
				return
			}

			svr.Logger.ErrorContext(ctx,
				"Error looking up verification details from the database",
				slog.String("userEmail", submission.Email),
				slog.String("token", submission.Code),
				slog.String("errorMessage", err.Error()),
			)
			err = deleteVerification(ctx, svr, submission.Email, nil)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", recData.email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/login-form.html", "login-form")
				return
			}
			writeResponse(ctx, res, req, span, verificationCtr, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")
			return

		} else {
			svr.Logger.DebugContext(ctx, "Read in record data", slog.String("recordEmail", recData.email))
		}

		emailsMatch, codesMatch, attemptsRemaining, beforeExpiration := compareValidation(recData, submission)
		svr.Logger.DebugContext(ctx,
			"Checked verification fields",
			slog.Bool("emailsMatch", emailsMatch),
			slog.Bool("codesMatch", codesMatch),
			slog.Bool("attemptsRemaining", attemptsRemaining),
			slog.Bool("beforeExpiration", beforeExpiration),
		)
		switch {

		case emailsMatch && codesMatch && !beforeExpiration:
			err = deleteVerification(ctx, svr, submission.Email, nil)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", recData.email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
			}
			writeResponse(ctx, res, req, span, verificationCtr, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")

		case emailsMatch && !codesMatch:
			if attemptsRemaining {

				submission.success = false
				submission.Errors.ErrorMessage = "There was a problem confirming your verification code, please re-enter the code and try again"
				updateAttemptCount(ctx, svr, recData.email, recData.attempts)
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")

			} else {

				err := deleteVerification(ctx, svr, submission.Email, nil)
				if err != nil {
					svr.Logger.ErrorContext(ctx,
						"Error cleaning up the verification table!",
						slog.String("userEmail", recData.email),
						slog.String("errorMessage", err.Error()),
					)
					submission.success = false
					submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
					writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
				}
				writeResponse(ctx, res, req, span, verificationCtr, svr, loginWithError(LoginFailed), "/login-form.html", "login-form")

			}

		default:
			/*
				Clean up the verification record so this code can't be re-used
			*/
			tx, err := svr.DB.BeginTx(ctx, nil)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error starting a transaction to build a validated session",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
			}

			err = deleteVerification(ctx, svr, recData.email, tx)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error cleaning up the verification table!",
					slog.String("userEmail", recData.email),
					slog.String("errorMessage", err.Error()),
				)
				rbErr := tx.Rollback()
				if rbErr != nil {
					panic(rbErr)
				}
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
			}

			sessionID, sessionExpires, err := createSession(ctx, svr, req, recData.email)
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error writing a session record!",
					slog.String("userEmail", recData.email),
					slog.String("errorMessage", err.Error()),
				)
				rbErr := tx.Rollback()
				if rbErr != nil {
					panic(rbErr)
				}
				submission.success = false
				submission.Errors.ErrorMessage = "Error completing login, please try again shortly"
				writeResponse(ctx, res, req, span, verificationCtr, svr, submission, "/verify-login.html", "verify-login-form")
			}
			err = tx.Commit()
			if err != nil {
				svr.Logger.ErrorContext(ctx,
					"Error committing the session initialization!",
					slog.String("userEmail", submission.Email),
					slog.String("errorMessage", err.Error()),
				)
			}

			cookie := http.Cookie{
				Name:     SessionCookie,
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

func createSession(ctx context.Context, svr *util.ServerUtils, req *http.Request, email string) (string, time.Time, error) {

	svr.Logger.InfoContext(ctx,
		"Starting a new authenticated session",
		slog.String("userEmail", email),
	)

	expires := time.Now().Add(5 * time.Minute).UTC()
	sessionID := rand.Text()
	userAgent := req.UserAgent()

	res, err := svr.DB.ExecContext(ctx, "INSERT INTO session(session_id, email, expiration, user_agent) VALUES ($1, $2, $3, $4)", sessionID, email, expires, userAgent)
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
		svr.Logger.ErrorContext(ctx,
			"Error getting the number of rows modified saving the session",
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

func deleteVerification(ctx context.Context, svr *util.ServerUtils, email string, tx *sql.Tx) error {

	/* Make sure we have a transaction so we can roll back if this doesn't work */
	commit := false
	if tx == nil {

		var err error
		tx, err = svr.DB.BeginTx(ctx, nil)
		if err != nil {
			svr.Logger.ErrorContext(ctx,
				"Error starting a transaction to remove a verification record from the database",
				slog.String("errorMessage",
					err.Error()),
			)
			return fmt.Errorf("could not start a transaction to remove a verification record: %v", err)
		}
		commit = true

	}

	res, err := svr.DB.ExecContext(ctx, "DELETE FROM verification WHERE email = $1", email)
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil {
			/*
				This should never happen, because the database could be in an invalid
				state, so panic
			*/
			panic(err)
		}
		svr.Logger.ErrorContext(ctx,
			"Error deleting the verification record",
			slog.String("errorMessage", err.Error()),
		)
		return fmt.Errorf("error executing the cleanup of the verification record: %v", err)
	}

	/* If we created the transaction here, commit it here */
	if commit {

		err = tx.Commit()
		if err != nil {
			svr.Logger.ErrorContext(ctx,
				"Error committing the verification cleanup transaction",
				slog.String("userEmail", email),
				slog.String("errorMessage", err.Error()),
			)
			if rbErr := tx.Rollback(); rbErr != nil {
				panic(rbErr)
			}
			return fmt.Errorf("error committing verification cleanup: %v", err)
		}

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
			slog.String("userEmail", email),
		)
	}

	return nil

}

func updateAttemptCount(ctx context.Context, svr *util.ServerUtils, email string, attempt int) error {

	attempt++

	tx, err := svr.DB.BeginTx(ctx, nil)
	if err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error starting the transaction to update verification attempts",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.String("errorMessage", err.Error()),
		)
		/*
			Swallowing the error because no DB operation actually started, so I don't
			care if the rollback fails
		*/
		_ = tx.Rollback()
		return fmt.Errorf("could not start the transaction to update the attempt count: %v", err)
	}

	res, err := svr.DB.ExecContext(ctx,
		"UPDATE verification SET attempts = $1 WHERE email = $2",
		attempt,
		email,
	)
	if res == nil || err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error updating the attempt count in the database",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.String("errorMessage", err.Error()),
		)
		rbErr := tx.Rollback()
		if rbErr != nil {
			panic(rbErr)
		}
	}

	if modified, err := res.RowsAffected(); err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error getting the list of rows modified after updating the verification attempt count",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.String("errorMessage", err.Error()),
		)
		/* Don't return an error, the UPDATE operation itself didn't fail */
	} else if modified != 1 {
		svr.Logger.ErrorContext(ctx,
			"Expected 1 (and only 1) row to have been modified!",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.Int64("rowsModified", modified),
		)
		rbErr := tx.Rollback()
		if rbErr != nil {
			panic(rbErr)
		}
	}

	err = tx.Commit()
	if err != nil {
		svr.Logger.ErrorContext(ctx,
			"Error writing the updated attempt count to the database",
			slog.String("userEmail", email),
			slog.Int("attemptNum", attempt),
			slog.String("errorMessage", err.Error()),
		)
		rbErr := tx.Rollback()
		if rbErr != nil {
			panic(rbErr)
		}
		return fmt.Errorf("could not commit the updated attempt count: %v", err)
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

func setVerificationCode(ctx context.Context, svr *util.ServerUtils, userData *loginForm) (int64, string, error) {

	/* Open a transaction so we can rollback on a DB write failure */
	tx, err := svr.DB.BeginTx(ctx, nil)
	if err != nil {
		svr.Logger.ErrorContext(ctx, "Error starting transaction", slog.String("errorMessage", err.Error()))
		/* Not capturing the error here because we haven't touched the DB yet */
		tx.Rollback()
		userData.success = false
		userData.Errors.ErrorMessage = "Error saving login"
		return 0, "", fmt.Errorf("could not start transaction to save verification token: %v", err)
	}

	token := rand.Text()
	expires := time.Now().Add(5 * time.Minute).UTC()
	svr.Logger.DebugContext(ctx, "Created a login token", slog.String("userEmail", userData.Email))

	rows, err := svr.DB.ExecContext(ctx, "INSERT INTO verification (token, token_expiration, email) VALUES ($1, $2, $3) ON CONFLICT (email) DO UPDATE SET token = $1, token_expiration = $2", token, expires, userData.Email)
	if err != nil {

		switch {

		/*
			This happens when an email not in the DB is submitted. Swallow it so we
			don't given away valid emails to a scan
		*/
		case strings.Contains(err.Error(), "violates foreign key constraint \"verification_email_fkey\""):
			svr.Logger.WarnContext(ctx,
				"Email not found in database. Postgres thinks it's an error, but we don't",
				slog.String("userEmail", userData.Email),
				slog.String("errorMessage", err.Error()),
			)
			return 0, "", nil

		default:
			svr.Logger.ErrorContext(ctx, "Error saving token information!", slog.String("errorMessage", err.Error()))
			userData.success = false
			rollbackErr := tx.Rollback()
			/*
				The rollback error warrants a panic because the database could be in an
				invalid state
			*/
			if rollbackErr != nil {
				panic(fmt.Errorf("error rolling back the token assignment transaction: %v", rollbackErr))
			}
			return 0, token, fmt.Errorf("could not write verification token information to the database: %v", err)

		}

	}

	tx.Commit()

	svr.Logger.DebugContext(ctx, "Ran the token INSERT command", slog.String("userEmail", userData.Email))
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

func compareValidation(record verificationRecord, submission verificationForm) (emailsMatch bool, tokensMatch bool, attemptsRemaining bool, beforeExpiration bool) {

	now := time.Now().UTC()

	emailsMatch = strings.EqualFold(record.email, submission.Email)
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
	span trace.Span,
	ctr metric.Int64Counter,
	svr *util.ServerUtils,
	submission Submitter,
	templateFile string,
	templateDef string,
) {

	ctr.Add(ctx, 1, metric.WithAttributes(
		attribute.Bool("successful", submission.succeeded()),
	))
	span.SetAttributes(
		attribute.Bool("successful", submission.succeeded()),
		attribute.String("email", submission.emailAddress()),
	)

	tmplPath := fmt.Sprintf("%s/%s", svr.Getenv("TEMPLATES_DIR"), templateFile)

	tmpl, tmplErr := template.ParseFiles(tmplPath)
	if tmplErr != nil {
		res.WriteHeader(500)
		res.Write([]byte("Error loading the login page template!"))
		return
	}

	/* TODO: SHOULD I MOVE THESE MESSAGES TO A MIDDLEWARE WITH A CONTEXT VARAIBLE REFERENCE FOR DATA? */
	svr.Logger.InfoContext(ctx,
		fmt.Sprintf("Finished the operation %s", req.URL.Path),
		slog.String("userData", submission.String()),
		slog.String("validationErrors", submission.Error()),
		slog.Bool("successful", submission.succeeded()),
	)

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
