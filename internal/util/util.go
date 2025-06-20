package util

import (
	"database/sql"
	"log/slog"
)

// ServerUtils represents a collection of references that are used in most, if
// not all, the back-end calls. Wrapping them up so that not every handler is
// taking a minimum of 3 paramaters.
type ServerUtils struct {
	DB     *sql.DB
	Getenv func(string) string
	Logger *slog.Logger
}
