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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type migrationFile struct {
	fs   fs.FS
	name string
}

const (
	FindMigrationsQuery      = "SELECT filename FROM migrations ORDER BY filename ASC"
	InsertMigrationStatement = "INSERT INTO migrations (filename, appliedOn) VALUES ($1, CURRENT_TIMESTAMP(3))"
)

var (
	ErrMigration = fmt.Errorf("could not apply database migration")
)

// Checks for any pending database migrations and applies them
func (dbConn DBConn) runMigrations(
	ctx context.Context,
	logger *slog.Logger,
	getenv func(string) string) (errs []error) {

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

	/* Capture metrics around the migrations run */
	var rowCount int64 = 0
	totalRowsImpacted, err := meter.Int64Counter(
		"migrations.rows.impacted",
		metric.WithDescription("Number of database rows impacted by this batch of migration scripts"),
		metric.WithUnit("{affected}"),
	)
	if err != nil {
		panic("could not initialize the total rows affected metric " + err.Error())
	}

	/*
		Find the list of migrations we've already applied so we don't duplicate them
	*/
	migrationsApplied, err := dbConn.readAppliedMigrations(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Error reading applied migrations from the database", slog.String("errorMessage", err.Error()))
		errs = append(errs, fmt.Errorf("error reading applied migrations from the database: %s", err.Error()))
		return
	}
	logger.DebugContext(ctx, "Have the list of migrations applied", slog.Any("migrationsApplied", migrationsApplied))

	/*
		Check the filesystem for migrations to run. Because the migration files
		start with a timestamp, sorting them will create an ordered list of
		migrations, the assumption being that at there's 1 point separating
		migrations that were applied from the ones that need to be applied
		(if a migration file failed earlier, the assumption breaks, but the code will
		still recover if that's the case)
	*/
	logger.DebugContext(ctx, "Listing the migrations files", slog.String("migrationsDirectory", getenv("MIGRATIONS_DIR")))
	dirList := strings.Split(getenv("MIGRATIONS_DIR"), ",")
	migrationFiles := []migrationFile{}

	/* Build a list of migration files across all templates */
	for _, dirName := range dirList {

		migrationsFS := os.DirFS(strings.TrimSpace(dirName))
		filesFound, err := listMigrations(migrationsFS, ".")
		if err != nil {
			logger.ErrorContext(ctx, "Error listing database migration files", slog.String("errorMessage", err.Error()))
			errs = append(errs, fmt.Errorf("error reading applied migrations from the database: %s", err.Error()))
			return
		}

		migrationFiles = append(migrationFiles, filesFound...)

	}

	if len(migrationFiles) < 1 {
		logger.InfoContext(ctx, "No SQL migrations to apply.", slog.String("migrationsDir", getenv("MIGRATIONS_DIR")))
		return
	}

	/* Sort alphabetically by filename */
	slices.SortFunc(migrationFiles, sortDirEntries)

	fileToRowsAffected := make(map[string]int64)
	for _, migration := range migrationFiles {

		/*
			The length of migrationsApplied is 0 when no migrations have been run yet,
			so we obviously need to apply anything we have in that case.
		*/
		if slices.Contains(migrationsApplied, migration.name) {

			logger.InfoContext(ctx, "Already applied migration, skipping...", slog.String("filename", migration.name))
			continue

		}

		/*
			Run any migrations not already logged in the database
		*/
		tx, err := dbConn.db.BeginTx(ctx, nil)
		if err != nil {
			logger.ErrorContext(ctx, "Error starting transaction", slog.String("errorMessage", err.Error()))
			errs = append(errs, fmt.Errorf("error starting transaction lock on the database migrations: %s", err.Error()))
			return
		}

		logger.InfoContext(ctx, "Applying migration file", slog.String("filename", migration.name))
		rowsAffected, err := dbConn.applyMigration(ctx, logger, migration)
		if err != nil {
			logger.ErrorContext(ctx, "Migration failed", slog.String("errorMessage", err.Error()))
			rollback(ctx, tx, logger, migration.name)
			errs = append(errs, fmt.Errorf("%w could not apply migration file: %s %w", ErrMigration, migration, err))
			continue
		}

		fileToRowsAffected[migration.name] = rowsAffected

		/* Log the migration to the database so we don't repeat it */
		logger.DebugContext(ctx, fmt.Sprintf("Adding %s to the database", migration.name))
		_, err = dbConn.Execute(ctx, InsertMigrationStatement, migration.name)
		if err != nil {
			logger.ErrorContext(
				ctx,
				"Error adding migration file to migrations table!",
				slog.String("filenam", migration.name),
				slog.String("errorMessage", err.Error()),
			)
			errs = append(errs, fmt.Errorf("%w: could not add migration file: %s to the list of migrations run", ErrMigration, migration.name))
			break
		}

		err = tx.Commit()
		if err != nil {
			rollback(ctx, tx, logger, migration.name)
			errs = append(errs, fmt.Errorf("error committing the migrations to the database: %w", err))
			break
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

	return

}

func (dbConn DBConn) applyMigration(
	ctx context.Context,
	logger *slog.Logger,
	migration migrationFile,
) (int64, error) {

	var totalRowsAffected int64 = 0
	sqlBytes, err := fs.ReadFile(migration.fs, migration.name)
	if err != nil {
		logger.ErrorContext(ctx, "Error reading data from migration file",
			slog.String("migrationFile", migration.name),
			slog.String("errorMessage", err.Error()))
		return 0, fmt.Errorf("error reading migration data: %s", err.Error())
	}

	rawMigration := string(sqlBytes)
	statements := strings.SplitSeq(rawMigration, ";")
	for sqlStatement := range statements {

		/*
			If the file ends with a ";", Go picks up an empty string as a "final" token.
		*/
		if len(strings.TrimSpace(sqlStatement)) < 1 {

			continue

		}

		result, err := dbConn.Execute(ctx, sqlStatement)
		if err != nil {
			logger.ErrorContext(ctx, "Error applying migration",
				slog.String("sqlStatement", sqlStatement),
				slog.String("errorMessage", err.Error()))
			return 0, fmt.Errorf("error applying migration statement \"%s\": %s", sqlStatement, err.Error())
		}

		if rowsAffected, err := result.RowsAffected(); err != nil {
			logger.ErrorContext(ctx, "Error getting the number of rows impacted",
				slog.String("errorMessage", err.Error()))
			return 0, fmt.Errorf("error getting the number of rows impacted by sql statement \"%s\": %s", sqlStatement, err.Error())
		} else {
			totalRowsAffected += rowsAffected
		}

	}

	logger.DebugContext(
		ctx,
		"Migration applied",
		slog.Int64("rowsAffected", totalRowsAffected),
		slog.String("filename", migration.name),
		slog.String("statement", rawMigration),
	)
	return totalRowsAffected, nil

}

func listMigrations(migrationsDir fs.FS, root string) ([]migrationFile, error) {

	sqlFiles := []migrationFile{}

	migrationFiles, err := fs.ReadDir(migrationsDir, root)
	if err != nil {
		return sqlFiles, fmt.Errorf("error building the list of migration files: %s", err)
	}

	for _, entry := range migrationFiles {

		/* Skip sub-directories */
		if entry.IsDir() {
			continue
		}

		sqlFiles = append(sqlFiles, migrationFile{fs: migrationsDir, name: entry.Name()})

	}

	return sqlFiles, nil

}

func (dbConn DBConn) readAppliedMigrations(ctx context.Context) ([]string, error) {

	var migratedFiles []string
	rows, err := dbConn.Query(ctx, FindMigrationsQuery)
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

func sortDirEntries(left migrationFile, right migrationFile) int {

	return strings.Compare(left.name, right.name)

}
