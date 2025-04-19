package server

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
)

type server struct {
	port   int
	getenv func(string) string
}

// Builds a new HTTP handler for the application. This will be used for testing and running the server
func NewServer(getenv func(string) string, db *sql.DB, logger *slog.Logger) (http.Handler, error) {

	port, err := strconv.Atoi(getenv("PORT"))
	if err != nil {
		logger.Error("Invalid server port.", slog.String("port", getenv("PORT")))
		panic(err)
	}

	appSrv := &server{
		port:   port,
		getenv: getenv,
	}

	handler, err := appSrv.registerRoutes(db, logger)
	if err != nil {
		logger.Error("Server failed to start", slog.String("errorMessage", err.Error()))
		return nil, err
	}

	return handler, nil

}
