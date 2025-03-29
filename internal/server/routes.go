package server

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"
)

// All HTTP routes go here so devs can get an overview of the application
func (svr *server) RegisterRoutes(db *sql.DB, logger *slog.Logger) (http.Handler, error) {

	/*
	   I'm using a vertical slice architecture, so the handler logic will be
	   split amongst several different packages. They'll all need to be
	   initialized before registering, so do that here.
	*/

	mux := http.NewServeMux()
	mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	mux.Handle("GET /health", HealthCheckHandler(svr.templateDir, db, logger))

	logger.Info("Registered all routes")
	return cors(mux, logger), nil

}

/* Sets the CORS response for all endpoints */
func cors(next http.Handler, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		logger.Info("Processing CORS", slog.String("requestURL", req.URL.String()), slog.String("pattern", req.Pattern))
		res.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOWED_HOSTS"))
		res.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		res.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		/* TODO: CHANGE TO TRUE WHEN CREDENTIALS ARE FIGURED OUT */
		res.Header().Set("Access-Control-Allow-Credentials", "false")

		if req.Method == http.MethodOptions {

			res.WriteHeader(http.StatusNoContent)
			return

		}

		next.ServeHTTP(res, req)

	})

}
