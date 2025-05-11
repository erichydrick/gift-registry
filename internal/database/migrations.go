package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	name = "net.hydrick.gift-registry/server"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var (
	meter  = otel.Meter(name)
	tracer = otel.Tracer(name)
)

// Checks for any pending database migrations and applies them
func RunMigrations(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
	getenv func(string) string) (map[string]int64, error) {

	ctx, span := tracer.Start(ctx, "RunMigrations")
	defer span.End()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	/* Handle a shutdown message coming in. */
	select {

	case <-ctx.Done():
		logger.WarnContext(ctx, "Received a termination signal, not running the migrations")
	default:
		/* Do nothing */

	}

	var rowCount int64 = 0
	totalRowsImpacted, err := meter.Int64Counter(
		"migrations.rows.impacted",
		metric.WithDescription("Number of database rows impacted by this batch of migration scripts"),
		metric.WithUnit("{affected}"),
	)
	if err != nil {
		panic("could not initialize the total rows affected metric " + err.Error())
	}

	migrationsRun, err := migrationsRun(ctx, db)
	if err != nil {
		logger.ErrorContext(ctx, "Error reading applied migrations from the database", slog.String("errorMessage", err.Error()))
		return map[string]int64{}, fmt.Errorf("error reading applied migrations from the database: %s", err.Error())
	}

	sqlFiles, err := listMigrations(migrationsFS, getenv("MIGRATIONS_DIR"))
	if err != nil {
		logger.ErrorContext(ctx, "Error reading applied migrations from the database", slog.String("errorMessage", err.Error()))
		return map[string]int64{}, fmt.Errorf("error reading applied migrations from the database: %s", err.Error())
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		logger.ErrorContext(ctx, "Error starting transaction", slog.String("errorMessage", err.Error()))
		return map[string]int64{}, fmt.Errorf("error starting transaction lock on the database migrations: %s", err.Error())
	}

	fileToRowsAffected := make(map[string]int64)
	recIndex := 0
	for _, sqlFile := range sqlFiles {

		/*
			Technically, if migrationsRun[recIndex] (where we are in the list of applied
			migrations per the DB) is < the file, it implies that a migration file was
			removed but is still "live" in the database. There's nothing we can do about
			that, just carry on wayward son.
		*/
		if migrationsRun[recIndex] <= sqlFile.Name() {
			recIndex++
			continue
		}

		rowsAffected, err := applyMigration(ctx, db, logger, migrationsFS, sqlFile)
		if err != nil {
			rollback(ctx, tx, logger, sqlFile.Name())
			break
		}

		fileToRowsAffected[sqlFile.Name()] = rowsAffected

		db.ExecContext(ctx, "INSERT INTO gift_registry.migrations (filename, appliedOn) VALUES ($1, CURRENT_TIMESTAMP(3))", sqlFile.Name(), time.Now().UTC())

	}

	attributes := make([]attribute.KeyValue, len(fileToRowsAffected))
	for key, value := range fileToRowsAffected {

		fileMetric, err := meter.Int64Counter(
			"migrations.rows.impacted",
			metric.WithDescription("Number of database rows impacted by this batch of migration scripts"),
			metric.WithUnit("{affected}"),
		)
		if err != nil {
			logger.ErrorContext(ctx, "Error building metric on the rows updated by migration script",
				slog.String("migrationFile", key),
				slog.String("errorMessage", err.Error()))
			rollback(ctx, tx, logger, key)
		}

		fileMetric.Add(ctx, value)
		attributes = append(attributes, attribute.Int64(key, value))

	}

	totalRowsImpacted.Add(ctx, rowCount, metric.WithAttributes(attributes...))
	attributes = append(attributes, attribute.Int64("totalRowsImpacted", rowCount))

	span.SetAttributes(attributes...)

	/* TODO: REMOVE THIS WHEN WE HAVE TESTS */
	return nil, nil
	// return fileToRowsAffected, nil

}

func applyMigration(ctx context.Context, db *sql.DB, logger *slog.Logger, migrations embed.FS, migrationFile fs.DirEntry) (int64, error) {

	sqlBytes, err := fs.ReadFile(migrations, migrationFile.Name())
	if err != nil {
		logger.ErrorContext(ctx, "Error reading data from migration file",
			slog.String("migrationFile", migrationFile.Name()),
			slog.String("errorMessage", err.Error()))
		return 0, fmt.Errorf("error reading migration data: %s", err.Error())
	}

	statement := string(sqlBytes)
	result, err := db.ExecContext(ctx, statement)
	if err != nil {
		logger.ErrorContext(ctx, "Error applying migration",
			slog.String("sqlStatement", statement),
			slog.String("errorMessage", err.Error()))
		return 0, fmt.Errorf("error applying migration statement \"%s\": %s", statement, err.Error())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.ErrorContext(ctx, "Error getting the number of rows impacted",
			slog.String("errorMessage", err.Error()))
		return 0, fmt.Errorf("error getting the number of rows impacted by sql statement \"%s\": %s", statement, err.Error())
	}

	return rowsAffected, nil

}

func listMigrations(migrationsDir embed.FS, root string) ([]fs.DirEntry, error) {

	migrationFiles, err := fs.ReadDir(migrationsDir, root)
	if err != nil {
		return migrationFiles, fmt.Errorf("error building the list of migration files: %s", err.Error())
	}

	/* The migrations directory should just be flat files, strip out subdirectories */
	migrationFiles = slices.DeleteFunc(migrationFiles, func(entry fs.DirEntry) bool {
		return entry.IsDir()
	})

	/* Sort alphabetically by filename */
	slices.SortFunc(migrationFiles, sortDirEntries)
	return migrationFiles, nil

}

func migrationsRun(ctx context.Context, db *sql.DB) ([]string, error) {

	var migratedFiles []string
	rows, err := db.QueryContext(ctx, "SELECT filename "+
		"	FROM gift_registry.migrations "+
		"	ORDER BY filename ASC")
	if err != nil {
		return migratedFiles, fmt.Errorf("error querying previous migrations from the database: %s", err.Error())
	}
	defer rows.Close()

	for rows.Next() {

		var filename string
		if err := rows.Scan(&filename); err != nil {
			return migratedFiles, fmt.Errorf("error mapping database filename %v to filename list: %s", rows, err.Error())
		}

		migratedFiles = append(migratedFiles, filename)

	}

	return migratedFiles, nil
}

func rollback(ctx context.Context, tx *sql.Tx, logger *slog.Logger, migrationFilename string) {

	err := tx.Rollback()
	if err != nil {
		logger.ErrorContext(ctx, "Error rolling migration query!", slog.String("migrationFile", migrationFilename), slog.String("errorMessage", err.Error()))
		/* Panicing here because the database may well be in an invalid state */
		panic(err)
	}

}

func sortDirEntries(left fs.DirEntry, right fs.DirEntry) int {

	switch {

	case left.IsDir() && !right.IsDir():
		return -1
	case !left.IsDir() && right.IsDir():
		return 1
	default:
		return strings.Compare(left.Name(), right.Name())

	}

}
