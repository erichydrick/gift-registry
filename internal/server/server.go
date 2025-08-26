package server

import (
	"database/sql"
	"fmt"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
)

var (
	appSrv  *util.ServerUtils
	emailer Emailer
)

// Builds a new HTTP hankrdler for the application. This will be used for testing and running the server
func NewServer(getenv func(string) string, db *sql.DB, logger *slog.Logger, emailProvider Emailer) (http.Handler, error) {

	emailer = emailProvider
	appSrv = &util.ServerUtils{
		DB:     db,
		Getenv: getenv,
		Logger: logger,
	}

	handler, err := registerRoutes()
	if err != nil {
		logger.Error("Server failed to start", slog.String("errorMessage", err.Error()))
		return nil, fmt.Errorf("error starting the server: %s", err.Error())
	}

	return handler, nil

}
