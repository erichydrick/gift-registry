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
type DBConn struct {
	DB       *sql.DB
	Hostname string
	Name     string
	Port     int
	Username string
}

// The database is configured through environment variables that should be set in the container
var (
	openConnections map[string]DBConn = map[string]DBConn{}
)

// Returns a singleton database connection, creating a new one if it's not already initialized. getenv() will use the container environment variables when running, but can be mocked for testing.
func Connection(ctx context.Context, logger *slog.Logger, getenv func(string) string) (DBConn, error) {

	port, err := strconv.Atoi(getenv("DB_PORT"))
	if err != nil {
		logger.ErrorContext(ctx, "Could not convert port value to integer", slog.String("portValue", getenv("DB_PORT")))
		return DBConn{}, fmt.Errorf("invalid port value: %s: %v", getenv("DB_PORT"), err)
	}

	dbConn := DBConn{
		Hostname: getenv("DB_HOST"),
		Port:     port,
		Name:     getenv("DB_NAME"),
		Username: getenv("DB_USER"),
	}

	connStr := url(getenv)

	/* Re-use this specific connection if we have it */
	if db, ok := openConnections[dbConn.String()]; ok && db.DB != nil {

		logger.InfoContext(
			ctx,
			"Have a connection reference, just need to make sure the DB reference is active",
			slog.String("dbURL", connStr),
		)
		/* Make sure there's an sql.DB pointer is "live" */
		if err = db.DB.Ping(); err != nil {

			logger.InfoContext(ctx, "DB reference closed, reopening...", slog.String("dbURL", connStr))
			sql, err := dbConn.open(ctx, logger, connStr)
			if err != nil {
				return DBConn{}, err
			}
			db.DB = sql

		}

		return openConnections[dbConn.String()], nil

	}

	logger.DebugContext(ctx, "Need to create a new connection with the connection URL", slog.String("dbURL", connStr))
	db, err := dbConn.open(ctx, logger, connStr)
	/* We can't run the application if we can't connect to the database, so go ahead and exit */
	if err != nil {
		return DBConn{}, err
	}

	dbConn.DB = db
	err = dbConn.runMigrations(ctx, logger, getenv)
	if err != nil {
		logger.ErrorContext(ctx, "Error applying migrations to database connection",
			slog.String("connectionURL", connStr),
			slog.String("errorMessage", err.Error()))
		/*
			I'm going to allow a connection to be returned (with the error) if the
			migration fails in the assumption that I'm in a degraded, but not
			unusable, state
		*/
		if !errors.Is(err, ErrMigration) {
			return DBConn{}, err
		}
	}
	logger.InfoContext(ctx, "Applied migrations to database connection",
		slog.String("connectionURL", connStr))

	openConnections[dbConn.String()] = dbConn

	/*
		err is the error from running the migration, send that back in case it
		failed, but at this point it's a MigrationError so we can confirm it's
		migration-related
	*/
	return dbConn, err

}

// Closes the database connection
func (dbConn *DBConn) Close() (err error) {

	if dbConn.DB != nil {

		err = dbConn.DB.Close()
		/*
			Clear the database connection reference so future calls to Connect()
			create a fresh connection
		*/
		if err == nil {
			var nilDB *sql.DB
			dbConn.DB = nilDB
		}

	}

	return

}

// Make the database connection type comparable for sorting. This is calculated
// using the connection URL for the connection.
func (dbConn DBConn) Compare(otherConn DBConn) int {

	return strings.Compare(dbConn.String(), otherConn.String())

}

// Make the database connection type comparable for equality. This is
// calculated using the usernames, hostnames, ports, database names,
// and whether the database pointer references are both nil or both
// non-nil.
func (dbConn DBConn) Equal(otherConn DBConn) bool {

	return dbConn.Hostname == otherConn.Hostname &&
		dbConn.Port == otherConn.Port &&
		dbConn.Username == otherConn.Username &&
		dbConn.Name == otherConn.Name &&
		((dbConn.DB == nil && otherConn.DB == nil) ||
			(dbConn.DB != nil && otherConn.DB != nil))

}

// Has the database connection type implement the Stringer interface
// Prints all the public fields along with a boolean indicating if the
// connection isn't nil
func (dbConn DBConn) String() string {

	return fmt.Sprintf(
		"{hostname: \"%s\", username: \"%s\", port: %d, password: *******, databaseName: \"%s\"}",
		dbConn.Hostname,
		dbConn.Username,
		dbConn.Port,
		dbConn.Name,
	)

}

func (dbConn DBConn) open(
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
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		getenv("DB_USER"),
		getenv("DB_PASS"),
		getenv("DB_HOST"),
		getenv("DB_PORT"),
		getenv("DB_NAME"),
	)

}
