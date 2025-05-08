package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	name = "net.hydrick.gift-registry/database"
)

var (
	ErrMigration = fmt.Errorf("could not apply database migration")

	meter  = otel.Meter(name)
	tracer = otel.Tracer(name)
)

// Checks for any pending database migrations and applies them
func (dbConn DBConn) runMigrations(
	ctx context.Context,
	logger *slog.Logger,
	getenv func(string) string) error {

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

	migrationsApplied, err := dbConn.readAppliedMigrations(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Error reading applied migrations from the database", slog.String("errorMessage", err.Error()))
		return fmt.Errorf("error reading applied migrations from the database: %s", err.Error())
	}
	logger.DebugContext(ctx, "Have the list of migrations applied", slog.Any("migrationsApplied", migrationsApplied))

	logger.Debug("Listing the migrations files", slog.String("migrationsDirectory", getenv("MIGRATIONS_DIR")))
	migrationsFS := os.DirFS(getenv("MIGRATIONS_DIR"))
	sqlFiles, err := listMigrations(migrationsFS, ".")
	if err != nil {
		logger.ErrorContext(ctx, "Error listing database migration files", slog.String("errorMessage", err.Error()))
		return fmt.Errorf("error reading applied migrations from the database: %s", err.Error())
	}

	if len(sqlFiles) < 1 {
		logger.InfoContext(ctx, "No SQL migrations to apply.", slog.String("migrationsDir", getenv("MIGRATIONS_DIR")))
		return nil
	}

	fileToRowsAffected := make(map[string]int64)
	var returnedErr error
	for _, sqlFile := range sqlFiles {

		if sqlFile.IsDir() {

			logger.InfoContext(ctx, "Skipping directory", slog.String("dirName", sqlFile.Name()))
			continue

		}

		/*
			The length of migrationsApplied is 0 when no migrations have been run yet,
			so we obviously need to apply anything we have in that case.

			Technically, if migrationsRun[recIndex] (where we are in the list of applied
			migrations per the DB) is < the file, it implies that a migration file was
			removed but is still "live" in the database. There's nothing we can do about
			that, just carry on wayward son.
		*/
		if slices.Contains(migrationsApplied, sqlFile.Name()) {

			logger.InfoContext(ctx, "Already applied migration, skipping...", slog.String("filename", sqlFile.Name()))
			continue

		}

		tx, err := dbConn.DB.BeginTx(ctx, nil)
		if err != nil {
			logger.ErrorContext(ctx, "Error starting transaction", slog.String("errorMessage", err.Error()))
			return fmt.Errorf("error starting transaction lock on the database migrations: %s", err.Error())
		}

		logger.InfoContext(ctx, "Applying migration file", slog.String("filename", sqlFile.Name()))
		rowsAffected, err := dbConn.applyMigration(ctx, logger, migrationsFS, sqlFile)
		if err != nil {
			rollback(ctx, tx, logger, sqlFile.Name())
			returnedErr = ErrMigration
			break
		}

		fileToRowsAffected[sqlFile.Name()] = rowsAffected

		logger.DebugContext(ctx, fmt.Sprintf("Adding %s to the database", sqlFile.Name()))
		_, err = dbConn.DB.ExecContext(ctx, "INSERT INTO migrations (filename, appliedOn) VALUES ($1, CURRENT_TIMESTAMP(3))", sqlFile.Name())
		if err != nil {
			logger.ErrorContext(ctx, "Error adding migration file to migrations table!", slog.String("filenam", sqlFile.Name()), slog.String("errorMessage", err.Error()))
			rollback(ctx, tx, logger, sqlFile.Name())
			returnedErr = ErrMigration
			break
		}

		/* Flush everything to the database */
		err = tx.Commit()
		if err != nil {
			logger.ErrorContext(ctx, "Error committing the database migration!", slog.String("errorMessage", err.Error()))
			returnedErr = ErrMigration
		}

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
		}

		fileMetric.Add(ctx, value)
		attributes = append(attributes, attribute.Int64(key, value))

	}

	totalRowsImpacted.Add(ctx, rowCount, metric.WithAttributes(attributes...))
	attributes = append(attributes, attribute.Int64("totalRowsImpacted", rowCount))

	span.SetAttributes(attributes...)

	return returnedErr

}

func (dbConn DBConn) applyMigration(
	ctx context.Context,
	logger *slog.Logger,
	migrations fs.FS,
	migrationFile fs.DirEntry) (int64, error) {

	sqlBytes, err := fs.ReadFile(migrations, migrationFile.Name())
	if err != nil {
		logger.ErrorContext(ctx, "Error reading data from migration file",
			slog.String("migrationFile", migrationFile.Name()),
			slog.String("errorMessage", err.Error()))
		return 0, fmt.Errorf("error reading migration data: %s", err.Error())
	}

	statement := string(sqlBytes)
	logger.DebugContext(ctx, "Applying SQL", slog.String("statement", statement))
	result, err := dbConn.DB.ExecContext(ctx, statement)
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
	logger.DebugContext(ctx, "Rows affected", slog.Int64("rowsAffected", rowsAffected), slog.String("filename", migrationFile.Name()), slog.String("statement", statement))

	return rowsAffected, nil

}

func listMigrations(migrationsDir fs.FS, root string) ([]fs.DirEntry, error) {

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

func (dbConn DBConn) readAppliedMigrations(ctx context.Context) ([]string, error) {

	var migratedFiles []string
	rows, err := dbConn.DB.QueryContext(ctx, "SELECT filename "+
		"	FROM migrations "+
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
