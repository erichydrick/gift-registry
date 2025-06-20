package server

import (
	"context"
	"database/sql"
	"fmt"
	"gift-registry/internal/health"
	"gift-registry/internal/registry"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type session struct {
	sessionID  string    `db:"session_id"`
	email      string    `db:"email"`
	expiration time.Time `db:"expiration"`
	userAgent  string    `db:"user_agent"`
}

const ()

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

// All HTTP routes go here so devs can get an overview of the application
func registerRoutes() (http.Handler, error) {

	mux := http.NewServeMux()

	handleFunc := func(pattern string, appHandler http.Handler) {

		handler := otelhttp.WithRouteTag(pattern, appHandler)
		mux.Handle(pattern, handler)

	}

	/* Static files */
	handleFunc("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(appSrv.Getenv("STATIC_FILES_DIR")+"/css"))))
	handleFunc("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(appSrv.Getenv("STATIC_FILES_DIR")+"/js"))))

	/* Base routes */
	handleFunc("GET /{$}", IndexHandler(appSrv))
	handleFunc("GET /health", health.HealthCheckHandler(appSrv))

	/* Authentication routes */
	handleFunc("GET /login", LoginFormHandler(appSrv))
	handleFunc("POST /login", LoginHandler(appSrv))
	handleFunc("POST /verify", VerificationHandler(appSrv))

	/* Registry routes */
	handleFunc("GET /registry", registry.RegistryHandler(appSrv))

	handler := otelhttp.NewHandler(cors(auth(mux)), "/")
	appSrv.Logger.Info("Registered all routes")
	return handler, nil

}

/*
TODO: MIDDLEWARES NEEDED:
2. RATE LIMITING (TO DEAL WITH SCRIPTS TRYING TO BRUTE FORCE CONF CODES)
*/

/* Enforces valid login sessions for non-public endpoints */
func auth(next http.Handler) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		pass := false

		/*
			The route is auth-protected, so query the DB to see if the session is
			present and valid
		*/
		cookie, err := req.Cookie(SessionCookie)
		if err != nil {
			appSrv.Logger.ErrorContext(ctx,
				"Error getting session ID from cookie",
				slog.Any("cookie", cookie),
				slog.String("errorMessage", err.Error()),
			)
			authNext(ctx, res, req, next, pass)
			return
		}

		now := time.Now().UTC()
		sessInfo, err := lookupSession(ctx, cookie.Value)
		if err != nil && err != sql.ErrNoRows {
			appSrv.Logger.ErrorContext(ctx,
				"Error loading session information",
				slog.String("cookieValue", cookie.Value),
				slog.String("errorMessage", err.Error()),
			)
			authNext(ctx, res, req, next, pass)
			return
		} else if sessInfo.sessionID == "" {

			appSrv.Logger.InfoContext(ctx,
				"No session info found, logging out",
				slog.String("cookieValue", cookie.Value),
			)
			authNext(ctx, res, req, next, pass)
			return

		}

		/* Verify the session hasn't expired */
		if sessInfo.expiration.Before(now) {

			appSrv.Logger.InfoContext(ctx,
				"Session has expired, logging out",
				slog.String("cookieValue", cookie.Value),
				slog.String("emailAddress", sessInfo.email),
			)
			authNext(ctx, res, req, next, pass)
			return

		}

		/* Cross check the user-agent with the 1 used to log in */
		if sessInfo.userAgent != req.UserAgent() {

			appSrv.Logger.InfoContext(ctx,
				"User agent doesn't match agent at sign-in. Logging out.",
				slog.String("cookieValue", cookie.Value),
				slog.String("emailAddress", sessInfo.email),
			)
			authNext(ctx, res, req, next, pass)
			return

		}

		/* Session's valid, continue the request */
		pass = true
		newExp := time.Now().Add(5 * time.Minute).UTC()
		cookie.MaxAge = int(time.Until(newExp).Seconds())
		http.SetCookie(res, cookie)
		extendSession(ctx, sessInfo.sessionID, newExp)
		authNext(ctx, res, req, next, pass)

	})

}

func authNext(
	ctx context.Context,
	res http.ResponseWriter,
	req *http.Request,
	next http.Handler,
	pass bool,
) {

	if pass || isPublic(ctx, req) {

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

		http.Redirect(res, req, "login", http.StatusSeeOther)
		return

	}

}

/* Sets the CORS response for all endpoints */
func cors(next http.Handler) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		appSrv.Logger.InfoContext(req.Context(), "Processing CORS", slog.String("requestURL", req.URL.String()), slog.String("pattern", req.Pattern))
		/* TODO: DO I NEED THIS? DOESN'T IT DEFAULT TO SAME HOST? */
		// res.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOWED_HOSTS"))
		res.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		res.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")

		if req.Method == http.MethodOptions {

			res.WriteHeader(http.StatusNoContent)
			return

		}

		appSrv.Logger.DebugContext(req.Context(), fmt.Sprintf("Now calling the %s handler for %s", req.Method, req.URL.Path))
		next.ServeHTTP(res, req)

	})

}

func extendSession(ctx context.Context, sessionID string, expires time.Time) error {

	tx, err := appSrv.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize database transaction: %v", err)
	}

	res, err := appSrv.DB.ExecContext(ctx, "UPDATE session SET expiration = $1 WHERE session_id = $2", expires, sessionID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error setting extended session expiration: %v", err)

	}

	if modified, err := res.RowsAffected(); err != nil {
		/* No rollback here, the write has been successful */
		appSrv.Logger.ErrorContext(ctx,
			"Error getting the number of rows modified from the update",
			slog.String("sessionID", sessionID),
			slog.String("errorMessage", err.Error()),
		)
	} else if modified != 1 {
		/* This potentially impacted too many records, roll back */
		tx.Rollback()
		appSrv.Logger.ErrorContext(ctx,
			"Unexpected number of rows updated",
			slog.Int64("updatedCount", modified),
			slog.String("sessionID", sessionID),
		)
	} else {
		appSrv.Logger.InfoContext(ctx,
			"Successfully set the updated expiration time in the database",
			slog.Int64("updatedCount", modified),
			slog.String("sessionID", sessionID),
		)
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error committing extended session expiration to database: %v", err)
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

func isPublic(ctx context.Context, req *http.Request) bool {

	for _, allowed := range publicRoutes {

		if allowed.Match([]byte(req.URL.Path)) {

			appSrv.Logger.InfoContext(ctx,
				"Public path, skipping auth check",
				slog.String("path", req.URL.Path),
				slog.String("pattern", allowed.String()),
			)

			return true

		}

	}

	return false

}
func lookupSession(ctx context.Context, sessionID string) (session, error) {

	var sessRec session
	err := appSrv.DB.QueryRowContext(ctx, "SELECT * FROM session WHERE session_id = $1", sessionID).Scan(&sessRec.sessionID, &sessRec.email, &sessRec.expiration, &sessRec.userAgent)
	/* Just returning an empty session to since that's the same as sql.ErrNoRows */
	if err != nil && err != sql.ErrNoRows {
		appSrv.Logger.ErrorContext(ctx,
			"Error looking up session information",
			slog.String("sessionID", sessionID),
			slog.String("errorMessage", err.Error()),
		)
		return session{}, fmt.Errorf("error looking up session information: %v", err)
	}

	return sessRec, nil

}
