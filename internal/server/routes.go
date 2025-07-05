package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// All HTTP routes go here so devs can get an overview of the application
func (svr ServerUtils) registerRoutes() (http.Handler, error) {

	/*
	   I'm using a vertical slice architecture, so the handler logic will be
	   split amongst several different packages. They'll all need to be
	   initialized before registering, so do that here.
	*/
	mux := http.NewServeMux()

	handleFunc := func(pattern string, appHandler http.Handler) {

		handler := otelhttp.WithRouteTag(pattern, appHandler)
		mux.Handle(pattern, handler)

	}

	/* Static files */
	handleFunc("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	handleFunc("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir("js"))))

	/* Base routes */
	handleFunc("GET /{$}", IndexHandler(svr))
	handleFunc("GET /health", HealthCheckHandler(svr))
	handleFunc("GET /signup", SignupFormHandler(svr))

	/* Person routes */
	/*
		TODO:
		SIGNUP ROUTE
		VERIFY EMAIL ROUTE
		LOGIN ROUTE
		VERIFY TOKEN ROUTE
	*/

	handler := otelhttp.NewHandler(cors(mux, svr), "/")
	svr.Logger.Info("Registered all routes")
	return handler, nil

}

/* Sets the CORS response for all endpoints */
func cors(next http.Handler, svr ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		svr.Logger.InfoContext(req.Context(), "Processing CORS", slog.String("requestURL", req.URL.String()), slog.String("pattern", req.Pattern))
		/* TODO: DO I NEED THIS? DOESN'T IT DEFAULT TO SAME HOST? */
		res.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOWED_HOSTS"))
		res.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		res.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		/* TODO: DOES THIS CHANGE IF I HANDLE SESSION MANAGEMENT */
		res.Header().Set("Access-Control-Allow-Credentials", "false")

		if req.Method == http.MethodOptions {

			res.WriteHeader(http.StatusNoContent)
			return

		}

		svr.Logger.DebugContext(req.Context(), fmt.Sprintf("Now calling the handler for %s", req.URL.Path))
		next.ServeHTTP(res, req)

	})

}
