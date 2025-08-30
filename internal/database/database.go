package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

// Represents the database connection and some other contextual information
// around the connection. Exposing the hostname, username, port, and database
// name publicly in case other packages need it.
type dbConn struct {
	db       *sql.DB
	hostname string
	name     string
	port     int
	username string
}

// A placeholder to use when I need an empty sql.Result object to represents
// a result to use after handling an error.
// For example, the login server uses this to treat not finding an email
// address the same as finding an email address
type EmptyResult struct{}

// The database is configured through environment variables that should be set in the container
var (
	connection dbConn
)

// Returns a singleton database connection, creating a new one if it's not already initialized. getenv() will use the container environment variables when running, but can be mocked for testing.
func Connection(ctx context.Context, logger *slog.Logger, getenv func(string) string) (*sql.DB, error) {

	port, err := strconv.Atoi(getenv("DB_PORT"))
	if err != nil {
		logger.ErrorContext(ctx, "Could not convert port value to integer", slog.String("portValue", getenv("DB_PORT")))
		return nil, fmt.Errorf("invalid port value: %s: %v", getenv("DB_PORT"), err)
	}

	logger.DebugContext(ctx,
		"Want to see if I have and existing connection",
		slog.String("connectionInfo", connection.String()))
	if connection.db != nil && connection.db.Ping() == nil {

		logger.InfoContext(
			ctx,
			"Have a connection reference, just need to make sure the DB reference is active",
			slog.String("connectionInfo", connection.String()),
		)
		return connection.db, nil

	}

	logger.DebugContext(ctx, "Need to create a new connection with the connection URL", slog.String("connectionInfo", connection.String()))
	connection = dbConn{
		hostname: getenv("DB_HOST"),
		port:     port,
		name:     getenv("DB_NAME"),
		username: getenv("DB_USER"),
	}
	db, err := connection.open(ctx, logger, url(getenv))
	/* We can't run the application if we can't connect to the database, so go ahead and exit */
	if err != nil {
		return nil, err
	}

	connection.db = db
	err = connection.runMigrations(ctx, logger, getenv)
	if err != nil {
		logger.ErrorContext(ctx, "Error applying migrations to database connection",
			slog.String("connectionDetails", connection.String()),
			slog.String("errorMessage", err.Error()))
		/*
			I'm going to allow a connection to be returned (with the error) if the
			migration fails in the assumption that I'm in a degraded, but not
			unusable, state
		*/
		if !errors.Is(err, ErrMigration) {
			return nil, err
		}
	}
	logger.InfoContext(ctx, "Applied migrations to database connection",
		slog.String("connectionDetails", connection.String()))

	/*
		err is the error from running the migration, send that back in case it
		failed, but at this point it's a MigrationError so we can confirm it's
		migration-related
	*/
	return connection.db, err

}

// Closes the database connection
func (conn *dbConn) Close() (err error) {

	if conn.db != nil {

		err = conn.db.Close()
		/*
			Clear the database connection reference so future calls to Connect()
			create a fresh connection
		*/
		if err == nil {
			var nilDB *sql.DB
			conn.db = nilDB
		}

	}

	return

}

// Make the database connection type comparable for sorting. This is calculated
// using the connection URL for the connection.
func (conn dbConn) Compare(otherConn dbConn) int {

	return strings.Compare(conn.String(), otherConn.String())

}

// Make the database connection type comparable for equality. This is
// calculated using the usernames, hostnames, ports, database names,
// and whether the database pointer references are both nil or both
// non-nil.
func (conn dbConn) Equal(otherConn dbConn) bool {

	return conn.hostname == otherConn.hostname &&
		conn.port == otherConn.port &&
		conn.username == otherConn.username &&
		conn.name == otherConn.name &&
		((conn.db == nil && otherConn.db == nil) ||
			(conn.db != nil && otherConn.db != nil))

}

// Has the database connection type implement the Stringer interface
// Prints all the public fields along with a boolean indicating if the
// connection isn't nil
func (conn dbConn) String() string {

	return fmt.Sprintf(
		"{hostname: \"%s\", username: \"%s\", port: %d, password: *******, databaseName: \"%s\"}",
		conn.hostname,
		conn.username,
		conn.port,
		conn.name,
	)

}

// Used to make EmptyResult compatible with sql.Result
// Just returns 0 with no error
func (er EmptyResult) LastInsertId() (int64, error) {

	return 0, nil

}

// Used to make EmptyResult compatible with sql.Result
// Just returns 0 with no error
func (er EmptyResult) RowsAffected() (int64, error) {

	return 0, nil

}

func (conn dbConn) open(
	ctx context.Context,
	logger *slog.Logger,
	url string) (*sql.DB, error) {

	db, err := sql.Open("postgres", url)
	if err != nil {
		logger.ErrorContext(ctx, "Error connecting to the database", slog.String("errorMessage", err.Error()))
		return nil, fmt.Errorf("could not connect to database: %v", err)
	}

	/*
		Connecting __looks__ successful even if the configs are bad. Confirm it
		worked by pinging the DB
	*/
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("could not successfully ping database connection %s: %v", url, err)
	}

	return db, nil

}

func url(getenv func(string) string) string {

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable&timezone=UTC",
		getenv("DB_USER"),
		getenv("DB_PASS"),
		getenv("DB_HOST"),
		getenv("DB_PORT"),
		getenv("DB_NAME"),
	)

}
