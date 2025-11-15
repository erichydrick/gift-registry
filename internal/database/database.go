package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Database interface {
	Close() error
	Execute(ctx context.Context, statement string, params ...any) (sql.Result, error)
	ExecuteBatch(ctx context.Context, statements []string, params [][]any) ([]sql.Result, []error)
	Ping(ctx context.Context) error
	Query(ctx context.Context, query string, params ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, params ...any) *sql.Row
}

// Represents the database connection and some other contextual information
// around the connection. Exposing the hostname, username, port, and database
// name publicly in case other packages need it.
type DBConn struct {
	db        *sql.DB
	histogram metric.Float64Histogram
	password  string
	username  string
	Hostname  string
	Name      string
	Port      int
}

const (
	name = "net.hydrick.gift-registry/database"
)

var (
	dbConn    DBConn
	histogram metric.Float64Histogram
	meter     = otel.Meter(name)
	tracer    = otel.Tracer(name)
)

func init() {

	var err error
	histogram, err = meter.Float64Histogram(
		name,
		metric.WithDescription("Measures query times in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}

}

// A placeholder to use when I need an empty sql.Result object to represents
// a result to use after handling an error.
// For example, the login server uses this to treat not finding an email
// address the same as finding an email address
type EmptyResult struct{}

// Wraps a sql.DB.ExecContext operation so we can capture the time it takes to
// perform the operation, and so other files don't have to handle the
// transaction logic
func (dbConn DBConn) Execute(
	ctx context.Context,
	statement string,
	params ...any,
) (sql.Result, error) {

	start := time.Now()

	ctx, span := tracer.Start(ctx, "DatabaseExecute")
	defer span.End()

	span.SetAttributes(attribute.String("query", statement), attribute.String("parameters", fmt.Sprintf("%v", params)))

	tx, err := dbConn.db.BeginTx(ctx, nil)
	if err != nil {
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("could not start a write-based transaction: %v", err)

	}

	res, err := dbConn.db.ExecContext(ctx, statement, params...)
	if err != nil {
		txFailure(ctx, tx, dbConn.histogram, start, err)
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("failed to perform database update: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		txFailure(ctx, tx, dbConn.histogram, start, err)
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("error committing the transaction: %v", err)
	}

	/* Capture the number of rows modified */
	if count, err := res.RowsAffected(); err == nil {
		span.SetAttributes(attribute.Int64("modifiedCount", count))
	}

	dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
	return res, nil

}

// Wraps multiple database operations in a series of sql.DB.ExecContext
// operations so we can capture the time it takes to perform the operation,
// and so other files don't have to handle the transaction logic
func (dbConn DBConn) ExecuteBatch(
	ctx context.Context,
	statements []string,
	params [][]any,
) (results []sql.Result, errors []error) {

	start := time.Now()
	results = make([]sql.Result, len(statements))
	errors = make([]error, len(statements))

	ctx, span := tracer.Start(ctx, "DatabaseExecute")
	defer span.End()

	tx, err := dbConn.db.BeginTx(ctx, nil)
	if err != nil {
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		results = append(results, EmptyResult{})
		errors = append(errors, err)
		span.End()
		return
	}

	for idx := range statements {

		/*
			Create per-query span details so I can easily find specific problem queries
			if needed. Using shadowing to (and try to) keep the code clean and easy to follow
		*/
		start := time.Now()
		ctx, span := tracer.Start(ctx, "QueryExecute")
		span.SetAttributes(attribute.String("query", statements[idx]), attribute.String("parameters", fmt.Sprintf("%v", params[idx]...)))

		res, err := dbConn.db.ExecContext(ctx, statements[idx], params[idx]...)
		if err != nil {
			txFailure(ctx, tx, dbConn.histogram, start, err)
			dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
			results = append(results, EmptyResult{})
			errors = append(errors, err)
			span.End()
			continue
		}

		/* Capture the number of rows modified */
		if count, err := res.RowsAffected(); err == nil {
			span.SetAttributes(attribute.Int64("modifiedCount", count))
		}

		errors = append(errors, nil)
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		span.End()

	}

	err = tx.Commit()
	if err != nil {
		txFailure(ctx, tx, dbConn.histogram, start, err)
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		results = append(results, EmptyResult{})
		errors = append(errors, err)
		span.End()
		return
	}

	dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
	return

}

// Wraps a call to sql.DB.Ping operation so everything is accessible from the interface. Not capturing the histogram since I'm not worried about performance on Ping().
func (dbConn DBConn) Ping(ctx context.Context) error {

	err := dbConn.db.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("error pinging the database: %v", err)
	}

	return nil

}

// Wraps a sql.DB.QueryContext operation so we can capture the time it takes to
// perform the operation
func (dbConn DBConn) Query(
	ctx context.Context,
	query string,
	params ...any,
) (*sql.Rows, error) {

	start := time.Now()

	ctx, span := tracer.Start(ctx, "DatabaseQuery")
	defer span.End()

	span.SetAttributes(attribute.String("query", query), attribute.String("parameters", fmt.Sprintf("%v", params)))

	rows, err := dbConn.db.QueryContext(ctx, query, params...)
	if err != nil {
		dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return nil, fmt.Errorf("error querying database: %v", err)
	}

	dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
	return rows, nil

}

// Wraps a sql.DB.QueryContext operation so we can capture the time it takes to
// perform the operation
func (dbConn DBConn) QueryRow(
	ctx context.Context,
	query string,
	params ...any,
) *sql.Row {

	start := time.Now()

	ctx, span := tracer.Start(ctx, "DatabaseQueryRow")
	defer span.End()

	span.SetAttributes(attribute.String("query", query), attribute.String("parameters", fmt.Sprintf("%v", params)))

	rows := dbConn.db.QueryRowContext(ctx, query, params...)
	dbConn.histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
	return rows

}

// Returns a singleton database connection, creating a new one if it's not already initialized. getenv() will use the container environment variables when running, but can be mocked for testing.
func Connection(ctx context.Context, logger *slog.Logger, getenv func(string) string) (Database, error) {

	/* Re-use this specific connection if we have it */
	if dbConn.db != nil {

		logger.InfoContext(
			ctx,
			"Have a connection reference, just need to make sure the DB reference is active",
			slog.String("dbHost", dbConn.Hostname),
			slog.Int("dbPort", dbConn.Port),
			slog.String("dbName", dbConn.Name),
		)

		/*
			This a sanity check to confirm that we're still connected to the database.
			It SHOULD always pass, but it's not paranoia if a variable reference could
			actually somehow close out from under you.
		*/
		if err := dbConn.Ping(ctx); err == nil {

			return dbConn, nil

		}

	}

	port, err := strconv.Atoi(getenv("DB_PORT"))
	if err != nil {
		logger.ErrorContext(ctx, "Could not convert port value to integer", slog.String("portValue", getenv("DB_PORT")))
		return nil, fmt.Errorf("invalid port value: %s: %v", getenv("DB_PORT"), err)
	}

	connStr := url(getenv)

	connection := DBConn{
		histogram: histogram,
		password:  getenv("DB_PASS"),
		username:  getenv("DB_USER"),
		Hostname:  getenv("DB_HOST"),
		Port:      port,
		Name:      getenv("DB_NAME"),
	}

	logger.DebugContext(ctx, "Need to create a new connection with the connection URL", slog.String("dbURL", connStr))
	db, err := connection.open(ctx, logger, connStr)
	/* We can't run the application if we can't connect to the database, so go ahead and exit */
	if err != nil {
		return DBConn{}, err
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
			unusable, state. For everything else we should just fail completely.
		*/

		if !errors.Is(err, ErrMigration) {
			return DBConn{}, err
		}

	}

	logger.InfoContext(ctx, "Applied migrations to database connection",
		slog.String("connectionDetails", connection.String()))

	/*
		err is the error from running the migration, send that back in case it
		failed (at this point we know it's a MigrationError)
	*/
	return connection, err

}

// Closes the database connection
func (dbConn DBConn) Close() (err error) {

	if dbConn.db != nil {

		err = dbConn.db.Close()
		if err == nil {
			dbConn = DBConn{}
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
		dbConn.password == otherConn.password &&
		dbConn.username == otherConn.username &&
		dbConn.Name == otherConn.Name &&
		((dbConn.db == nil && otherConn.db == nil) ||
			(dbConn.db != nil && otherConn.db != nil))

}

// Has the database connection type implement the Stringer interface
// Prints all the public fields along with a boolean indicating if the
// connection isn't nil
func (dbConn DBConn) String() string {

	return fmt.Sprintf(
		"{hostname: \"%s\", username: \"%s\", port: %d, password: *******, databaseName: \"%s\"}",
		dbConn.Hostname,
		dbConn.username,
		dbConn.Port,
		dbConn.Name,
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

/*
Opens a connection to the Postgres database and returns it.
*/
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

/*
Rolls back the given transaction if there's an error with a database
operation. Include the original error so it's not lost if the rollback
fails.
*/
func txFailure(
	ctx context.Context,
	tx *sql.Tx,
	histogram metric.Float64Histogram,
	start time.Time,
	err error) {

	rbErr := tx.Rollback()
	if rbErr != nil {
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		panic(fmt.Sprintf("error roll back a transaction: %v (original error: %v)", rbErr, err))
	}

}

/*
Builds a Postgres connection URL from the environment variables and returns
it.
*/
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
