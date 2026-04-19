package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	_ "github.com/tursodatabase/turso-go"
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
	Name   string
	db     *sql.DB
	logger *slog.Logger
}

const (
	/* DB cleanup happens every 5 minutes by default */
	defaultTickInterval       = 300000
	cleanupSessions           = "DELETE FROM session WHERE expiration <= CURRENT_TIMESTAMP"
	cleanupVerificationTokens = "DELETE FROM verification WHERE token_expiration <= CURRENT_TIMESTAMP"
	name                      = "net.hydrick.gift-registry/database"
)

var (
	dbConn    DBConn
	histogram metric.Float64Histogram
	meter     = otel.Meter(name)
	mutex     sync.Mutex
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
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("could not start a write-based transaction: %v", err)

	}

	if params == nil {
		params = []any{}
	}
	res, err := dbConn.db.ExecContext(ctx, statement, params...)
	if err != nil {
		txFailure(ctx, tx, histogram, start, err)
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("failed to perform database update: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		txFailure(ctx, tx, histogram, start, err)
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return EmptyResult{}, fmt.Errorf("error committing the transaction: %v", err)
	}

	/* Capture the number of rows modified */
	if count, err := res.RowsAffected(); err == nil {
		span.SetAttributes(attribute.Int64("modifiedCount", count))
	}

	histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
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
	errors = []error{}

	ctx, span := tracer.Start(ctx, "DatabaseExecute")
	defer span.End()

	tx, err := dbConn.db.BeginTx(ctx, nil)
	if err != nil {
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
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
			txFailure(ctx, tx, histogram, start, err)
			histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
			results = append(results, EmptyResult{})
			errors = append(errors, err)
			span.End()
			return
		}

		/* Capture the number of rows modified */
		if count, err := res.RowsAffected(); err == nil {
			span.SetAttributes(attribute.Int64("modifiedCount", count))
		}

		errors = append(errors, nil)
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		span.End()

	}

	err = tx.Commit()
	if err != nil {
		txFailure(ctx, tx, histogram, start, err)
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		results = append(results, EmptyResult{})
		errors = append(errors, err)
		span.End()
		return
	}

	histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
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
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		return nil, fmt.Errorf("error querying database: %v", err)
	}

	histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
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
	histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
	return rows
}

// Returns a singleton database connection, creating a new one if it's not already initialized. getenv() will use the container environment variables when running, but can be mocked for testing.
func Connect(ctx context.Context, logger *slog.Logger, getenv func(string) string) (Database, error) {

	mutex.Lock()
	defer mutex.Unlock()

	dbName := getenv("DB_NAME")

	/* Re-use this specific connection if we have it */
	if dbConn.db != nil {

		logger.InfoContext(
			ctx,
			"Have a connection reference, just need to make sure the DB reference is active",
			slog.String("filename", dbConn.Name),
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

	/*
		There's an os.ErrNotExist error I COULD check for, but other errors likely
		indicate problems reading from the file, which is just as bad.
	*/

	if _, err := os.Stat(dbName); err != nil {
		return DBConn{},
			fmt.Errorf("error checking for db file %s: %v", dbName, os.ErrNotExist)
	}

	connStr := "file:" + dbName

	connection := DBConn{
		Name:   getenv("DB_NAME"),
		logger: logger,
	}

	logger.DebugContext(ctx, "Need to create a new connection with the connection URL", slog.String("dbURL", connStr))
	db, err := connection.open(ctx, connStr)
	if err != nil {
		return DBConn{}, fmt.Errorf("could not open database connection to %s: %v", dbName, err)
	}

	connection.db = db
	errs := connection.runMigrations(ctx, getenv)
	onlyMigrationErrors := true
	if len(errs) > 0 {
		for _, err := range errs {
			logger.ErrorContext(ctx, "Error applying migrations to database connection",
				slog.String("filename", connection.Name),
				slog.String("errorMessage", err.Error()))
			/*
				I'm going to allow a connection to be returned (with the error) if the
				migration fails in the assumption that I'm in a degraded, but not
				unusable, state. For everything else we should just fail completely.

				This may well change in the future, but for now I'll allow it.
			*/

			onlyMigrationErrors = onlyMigrationErrors && errors.Is(err, ErrMigration)

		}
		if !onlyMigrationErrors {
			return DBConn{}, err
		}

	}

	logger.InfoContext(
		ctx,
		"Applied migrations to database connection",
		slog.String("filename", connection.Name),
	)

	/*
		Periodically remove expired sessions and verification tokens from the databse
	*/
	intStr := getenv("TICKER_INTERVAL")
	interval := defaultTickInterval
	if parsed, err := strconv.Atoi(intStr); err == nil {
		interval = parsed
	}
	ticker := time.NewTicker(time.Millisecond * time.Duration(interval))
	go cleanup(ctx, connection, ticker)

	/*
		err is the error from running the migration, send that back in case it
		failed (at this point we know it's a MigrationError)
	*/
	return connection, err
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

func cleanup(
	ctx context.Context,
	db DBConn,
	ticker *time.Ticker,
) {
	for {
		select {
		/* Time to clean up expired session and login verification tokens */
		case <-ticker.C:
			deleteQueries := []string{cleanupVerificationTokens, cleanupSessions}
			deleteParams := []any{}
			_, errList := db.ExecuteBatch(ctx, deleteQueries, [][]any{deleteParams, deleteParams})
			for _, err := range errList {
				if err == nil {
					continue
				}
				db.logger.ErrorContext(
					ctx,
					"Error cleaning expired sessions and verification tokens",
					slog.String("errorMessage", err.Error()),
				)
			}
		/* The app is shutting down, stop polling to clean up old tokens */
		case <-ctx.Done():
			ticker.Stop()
		}
	}
}

/*
Opens a connection to the Postgres database and returns it.
*/
func (dbConn DBConn) open(
	ctx context.Context,
	url string,
) (*sql.DB, error) {

	db, err := sql.Open("turso", url)
	if err != nil {
		dbConn.logger.ErrorContext(ctx, "Error connecting to the database", slog.String("errorMessage", err.Error()))
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
	err error,
) {
	rbErr := tx.Rollback()
	if rbErr != nil {
		histogram.Record(ctx, float64(time.Since(start).Milliseconds()))
		panic(fmt.Sprintf("error roll back a transaction: %v (original error: %v)", rbErr, err))
	}
}
