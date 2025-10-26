package util

import (
	"gift-registry/internal/database"
	"log/slog"
)

/*
TODO: SHOULD OTHER PACKAGES HAVE THEIR OWN COPIES OF THESE VARIABLES SO I'M NOT PASSING THIS OBJECT AROUND EVERYWHERE?

E.G. THE REGISTRY PACKAGE HAS A SETUP(DB DBCONN, GETENV FUNC(STRING) STRING, LOGGER *SLOG.LOGGER) THAT SETS THEM AT THE PACKAGE LEVEL AND IS AVAILABLE WITHOUT PASSING?
*/

// ServerUtils represents a collection of references that are used in most, if
// not all, the back-end calls. Wrapping them up so that not every handler is
// taking a minimum of 3 paramaters.
type ServerUtils struct {
	DB     database.Database
	Getenv func(string) string
	Logger *slog.Logger
}
