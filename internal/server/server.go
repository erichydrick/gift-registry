package server

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
)

// ServerUtils represents a collection of references that are used in most, if
// not all, the back-end calls. Wrapping them up so that not every handler is
// taking a minimum of 3 paramaters.
type ServerUtils struct {
	DB     *sql.DB
	Getenv func(string) string
	Logger *slog.Logger
}

// Builds a new HTTP handler for the application. This will be used for testing and running the server
func NewServer(getenv func(string) string, db *sql.DB, logger *slog.Logger) (http.Handler, error) {

	appSrv := &ServerUtils{
		DB:     db,
		Getenv: getenv,
		Logger: logger,
	}

	handler, err := appSrv.registerRoutes()
	if err != nil {
		logger.Error("Server failed to start", slog.String("errorMessage", err.Error()))
		return nil, fmt.Errorf("error starting the server: %s", err.Error())
	}

	return handler, nil

}
