package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/util"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

const (
	DeleteSessionQuery = "DELETE FROM session WHERE session_id = $1"
	ExtendSessionQuery = "UPDATE session SET expiration = $1 WHERE session_id = $2"
	LookupSessionQuery = "SELECT session_id, person_id, expiration, user_agent FROM session WHERE session_id = $1"
	SessionCookie      = "gift-registry-session"
)

type personKey int

type session struct {
	sessionID  string    `db:"session_id"`
	personID   int64     `db:"person_id"`
	expiration time.Time `db:"expiration"`
	userAgent  string    `db:"user_agent"`
}

const (
	_ personKey = iota
	loggedInUser
)

var (
	publicRoutes  []*regexp.Regexp
	routePatterns = []string{"^/$", "/css/*", "/health", "/js/*", "/login", "/verify"}
)

func init() {
	for _, pattern := range routePatterns {
		r, err := regexp.Compile(pattern)
		if err != nil {
			log.Println("Error initializing pattern matcher for", pattern, ", skipping")
			continue
		}
		publicRoutes = append(publicRoutes, r)
	}
}

// Enforces valid login sessions for non-public endpoints
func Auth(svr *util.ServerUtils, next http.Handler) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		pass := false

		/*
			The route is auth-protected, so query the DB to see if the session is
			present and valid
		*/
		cookie, err := req.Cookie(SessionCookie)
		if err != nil {
			/* No session data found, send the user to the login page */
			authNext(ctx, svr, res, req, next, pass)
			return
		}

		now := time.Now().UTC()
		sessInfo, err := lookupSession(ctx, svr, cookie.Value)
		if err != nil && err != sql.ErrNoRows {
			svr.Logger.ErrorContext(ctx,
				"Error loading session information",
				slog.String("cookieValue", cookie.Value),
				slog.String("errorMessage", err.Error()),
			)
			authNext(ctx, svr, res, req, next, pass)
			return
		} else if sessInfo.sessionID == "" {

			svr.Logger.InfoContext(ctx,
				"No session info found, logging out",
				slog.String("cookieValue", cookie.Value),
			)
			authNext(ctx, svr, res, req, next, pass)
			return

		}

		/* Verify the session hasn't expired */
		if sessInfo.expiration.Before(now) {

			svr.Logger.InfoContext(ctx,
				"Session has expired, logging out",
				slog.String("cookieValue", cookie.Value),
				slog.Int64("personID", sessInfo.personID),
			)
			deleteSession(ctx, svr, sessInfo.sessionID)
			authNext(ctx, svr, res, req, next, pass)
			return

		}

		/* Cross check the user-agent with the 1 used to log in */
		if sessInfo.userAgent != req.UserAgent() {

			svr.Logger.InfoContext(ctx,
				"User agent doesn't match agent at sign-in. Logging out.",
				slog.String("cookieValue", cookie.Value),
				slog.Int64("personID", sessInfo.personID),
			)
			deleteSession(ctx, svr, sessInfo.sessionID)
			authNext(ctx, svr, res, req, next, pass)
			return

		}

		/* Session's valid, continue the request */
		pass = true
		newExp := time.Now().Add(5 * time.Minute).UTC()
		cookie.MaxAge = int(time.Until(newExp).Seconds())
		http.SetCookie(res, cookie)
		extendSession(ctx, svr, sessInfo.sessionID, newExp)
		ctx = context.WithValue(ctx, loggedInUser, sessInfo.personID)
		req = req.WithContext(ctx)
		authNext(ctx, svr, res, req, next, pass)

	})

}

func PersonID(res http.ResponseWriter, req *http.Request) int64 {

	return req.Context().Value(loggedInUser).(int64)

}

func authNext(
	ctx context.Context,
	svr *util.ServerUtils,
	res http.ResponseWriter,
	req *http.Request,
	next http.Handler,
	pass bool,
) {

	if pass || isPublic(ctx, svr, req) {

		/*
			Redirect straight to the registry if trying to load the login page with a
			valid session
		*/
		if pass && isLogin(req.URL.Path) {

			http.Redirect(res, req, "registry", http.StatusSeeOther)
			return

		} else {

			next.ServeHTTP(res, req)
			return

		}

	} else {

		/* TODO: MAKE THIS A HARD REDIRECT TO /LOGIN */
		http.Redirect(res, req, "login", http.StatusSeeOther)
		return

	}

}

func deleteSession(ctx context.Context, svr *util.ServerUtils, sessionID string) error {

	svr.Logger.InfoContext(
		ctx,
		"Deleting existing session information",
		slog.String("sessionID", sessionID),
	)

	if result, err := svr.DB.Execute(ctx, DeleteSessionQuery, sessionID); err != nil {
		return fmt.Errorf("could not delete session information from the database: %v", err)
	} else if modified, err := result.RowsAffected(); err != nil {
		/*
			This error doesn't represent a failure to delete the session information,
			so still going to return nil, but I want to capture it in the logs just in
			case
		*/
		svr.Logger.WarnContext(
			ctx,
			"Could not the number of rows modified",
			slog.String("errorMessage", err.Error()),
		)
	} else if modified != 1 {
		/*
			Again, the operation didn't fail per se, but this isn't expected and we
			should be aware of it.

			In the immediate term, this will likely fire as a false positive until I
			get session/token cleanup automation implemented.
		*/
		svr.Logger.WarnContext(
			ctx,
			"Session deletion did not modify the expected number of records",
			slog.Int64("expectedCount", 1),
			slog.Int64("actualCount", modified),
		)
	}

	return nil

}

func extendSession(ctx context.Context, svr *util.ServerUtils, sessionID string, expires time.Time) error {

	res, err := svr.DB.Execute(ctx, ExtendSessionQuery, expires, sessionID)
	if err != nil {
		return fmt.Errorf("error setting extended session expiration: %v", err)
	}

	if modified, err := res.RowsAffected(); err != nil {
		/* No rollback here, the write has been successful */
		svr.Logger.ErrorContext(ctx,
			"Error getting the number of rows modified from the update",
			slog.String("sessionID", sessionID),
			slog.String("errorMessage", err.Error()),
		)
		/* TODO: WARN ON MODIFIED != 1 */
	} else {
		svr.Logger.InfoContext(ctx,
			"Successfully set the updated expiration time in the database",
			slog.Int64("updatedCount", modified),
			slog.String("sessionID", sessionID),
		)
	}

	return nil

}

func isLogin(path string) bool {

	loginPath, err := regexp.Compile("/login")
	if err != nil {
		return false
	}

	verifyPath, err := regexp.Compile("/verify")
	if err != nil {
		return false
	}

	return loginPath.Match([]byte(path)) || verifyPath.Match([]byte(path))

}

func isPublic(ctx context.Context, svr *util.ServerUtils, req *http.Request) bool {

	for _, allowed := range publicRoutes {

		if allowed.Match([]byte(req.URL.Path)) {

			svr.Logger.InfoContext(ctx,
				"Public path, skipping auth check",
				slog.String("path", req.URL.Path),
				slog.String("pattern", allowed.String()),
			)

			return true

		}

	}

	return false

}

func lookupSession(ctx context.Context, svr *util.ServerUtils, sessionID string) (session, error) {

	var sessRec session
	err := svr.DB.
		QueryRow(ctx, LookupSessionQuery, sessionID).
		Scan(&sessRec.sessionID, &sessRec.personID, &sessRec.expiration, &sessRec.userAgent)
	/* Just returning an empty session to since that's the same as sql.ErrNoRows */
	if err != nil && err != sql.ErrNoRows {
		svr.Logger.ErrorContext(ctx,
			"Error looking up session information",
			slog.String("sessionID", sessionID),
			slog.String("errorMessage", err.Error()),
		)
		return session{}, fmt.Errorf("error looking up session information: %v", err)
	}

	return sessRec, nil

}
