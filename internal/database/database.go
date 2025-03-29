package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// The database is configured through environment variables that should be set in the container
var (
	dbConn *sql.DB
)

// Returns a singleton database connection, creating a new one if it's not already initialized. getenv() will use the container environment variables when running, but can be mocked for testing.
func Connection(getenv func(string) string) (*sql.DB, error) {

	/* Re-use the existing connection once established */
	if dbConn != nil {
		return dbConn, nil
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		getenv("DB_USER"),
		getenv("DB_PASS"),
		getenv("DB_HOST"),
		getenv("DB_PORT"),
		getenv("DB_NAME"))
	db, err := sql.Open("postgres", connStr)
	/* We can't run the application if we can't connect to the database, so go ahead and exit */
	if err != nil {
		return nil, err
	}

	/*
		Connecting __looks__ successful even if the configs are bad. Confirm it
		worked by pinging the DB
	*/
	if err = db.Ping(); err != nil {
		return nil, err
	}

	dbConn = db
	return dbConn, nil

}

// Closes the database connection
func Close() error {

	return dbConn.Close()

}
