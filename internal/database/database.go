package database

import (
	"context"
	"database/sql"
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
	/* Not exposing outside the package, because it's a password... */
	password string
}

// The database is configured through environment variables that should be set in the container
var (
	openConnections map[string]DBConn
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
		password: getenv("DB_PASSWORD"),
	}

	connStr := dbConn.url()
	db, err := sql.Open("postgres", connStr)
	/* We can't run the application if we can't connect to the database, so go ahead and exit */
	if err != nil {
		return DBConn{}, err
	}

	/*
		Connecting __looks__ successful even if the configs are bad. Confirm it
		worked by pinging the DB
	*/
	if err = db.Ping(); err != nil {
		return DBConn{}, err
	}

	dbConn.DB = db
	return dbConn, nil

}

// Closes the database connection
func (dbConn DBConn) Close() (err error) {

	if dbConn.DB != nil {

		err = dbConn.DB.Close()
		/*
			Clear the database connection reference so future calls to Connect()
			create a fresh connection
		*/
		if err == nil {
			dbConn.DB = nil
		}

	}

	return

}

// Make the database connection type comparable for sorting. This is calculated
// using the connection URL for the connection.
func (dbConn DBConn) Compare(otherConn DBConn) int {

	return strings.Compare(dbConn.url(), otherConn.url())

}

// Make the database connection type comparable for equality. This is
// calculated using the connection URL for the connection, and whether
// the database pointer references are both nil or both non-nil.
func (dbConn DBConn) Equal(otherConn DBConn) bool {

	return dbConn.url() == otherConn.url() &&
		((dbConn.DB == nil && otherConn.DB == nil) ||
			(dbConn.DB != nil && otherConn.DB != nil))

}

// Has the database connection type implement the Stringer interface
// Prints all the public fields along with a boolean indicating if the
// connection isn't nil
func (dbConn DBConn) String() string {

	return fmt.Sprintf(
		"{databasePresent: %v, hostname: \"%s\", username: \"%s\", port: %d, password: *******, databaseName: \"%s\"}",
		dbConn.DB != nil,
		dbConn.Hostname,
		dbConn.Username,
		dbConn.Port,
		dbConn.Name,
	)

}

func (dbConn DBConn) url() string {

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbConn.Username,
		dbConn.password,
		dbConn.Hostname,
		dbConn.Port,
		dbConn.Name,
	)

}
