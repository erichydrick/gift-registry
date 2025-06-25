package server

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
)

type ServerUtils struct {
	DB     *sql.DB
	Getenv func(string) string
	Logger *slog.Logger
}

// Builds a new HTTP handler for the application. This will be used for testing and running the server
func NewServer(getenv func(string) string, db *sql.DB, logger *slog.Logger) (http.Handler, error) {

	/*
		port, err := strconv.Atoi(getenv("PORT"))
		if err != nil {
			logger.Error("Invalid server port.", slog.String("port", getenv("PORT")))
			panic(err)
		}
	*/

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
