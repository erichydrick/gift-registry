package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type migrationFile interface {
	name() string
	readFile() ([]byte, error)
}

type dataMigrationFile struct {
	fs       fs.FS
	filename string
}

type schemaMigrationFile struct {
	fs       embed.FS
	filename string
}

const (
	FindMigrationsQuery      = "SELECT filename FROM migrations ORDER BY filename ASC"
	InsertMigrationStatement = "INSERT INTO migrations (filename, applied_on) VALUES (?, CURRENT_TIMESTAMP)"
)

//go:embed migrations/*
var migrationsFS embed.FS

var ErrMigration = fmt.Errorf("could not apply database migration")

// Checks for any pending database migrations and applies them
func (dbConn DBConn) runMigrations(
	ctx context.Context,
	getenv func(string) string,
) (errs []error) {
	ctx, span := tracer.Start(ctx, "RunMigrations")
	defer span.End()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	/* Handle a shutdown message coming in. */
	select {

	case <-ctx.Done():
		dbConn.logger.WarnContext(ctx, "Received a termination signal, not running the migrations")
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
		createTblErr := dbConn.createMigrationsTable(ctx)
		if createTblErr != nil {
			dbConn.logger.ErrorContext(
				ctx,
				"Error reading applied migrations from the database",
				slog.String("errorMessage", err.Error()),
			)
			errs = append(
				errs,
				fmt.Errorf("error reading applied migrations from the database: %s",
					err.Error()),
			)
			return
		}
	}

	dbConn.logger.DebugContext(
		ctx,
		"Have the list of migrations applied",
		slog.Any("migrationsApplied", migrationsApplied),
	)

	/*
		Check the filesystem for migrations to run. Because the migration files
		start with a timestamp, sorting them will create an ordered list of
		migrations, the assumption being that at there's 1 point separating
		migrations that were applied from the ones that need to be applied
		(if a migration file failed earlier, the assumption breaks, but the code will
		still recover if that's the case)
	*/
	dbConn.logger.DebugContext(
		ctx,
		"Listing the migration files",
		slog.String("dataMigrationsDirectory", getenv("DATA_MIGRATIONS")),
	)

	/*
		TODO:
		2 oprations, 1 to read the schema migrations embedded in the executable, and
		1 to read data migrations from a folder on disk.
	*/
	migrationFiles, err := listSchemaMigrations(migrationsFS)
	if err != nil {
		return []error{fmt.Errorf("could not read database migration files: %v", err)}
	}
	dbConn.logger.DebugContext(
		ctx,
		"HAVE SCHEMA MIGRATION FILES",
		slog.Any("files", migrationFiles),
	)

	/*
		Data migrations (migrations around records in the database), are optional
		(as compared to database schema changes).
	*/
	if dataMigrationsDir := getenv("DATA_MIGRATIONS"); dataMigrationsDir != "" {

		dataMigrationsFS := os.DirFS(strings.TrimSpace(dataMigrationsDir))
		filesFound, err := listDataMigrations(dataMigrationsFS, ".")
		if err != nil {
			return []error{fmt.Errorf("error reading data migration files: %v", err)}
		}

		migrationFiles = append(migrationFiles, filesFound...)

	}
	dbConn.logger.DebugContext(
		ctx,
		"HAVE ALL MIGRATION FILES",
		slog.Any("files", migrationFiles),
	)

	if len(migrationFiles) < 1 {
		dbConn.logger.InfoContext(
			ctx,
			"No SQL migrations to apply.",
		)
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
		if slices.Contains(migrationsApplied, migration.name()) {

			dbConn.logger.InfoContext(
				ctx,
				"Already applied migration, skipping...",
				slog.String("filename", migration.name()),
			)
			continue

		}

		/*
			Run any migrations not already logged in the database
		*/
		tx, err := dbConn.db.BeginTx(ctx, nil)
		if err != nil {
			dbConn.logger.ErrorContext(
				ctx,
				"Error starting transaction",
				slog.String("errorMessage", err.Error()),
			)
			errs = append(
				errs,
				fmt.Errorf("error starting transaction lock on the database migrations: %s",
					err.Error()),
			)
			return
		}

		dbConn.logger.InfoContext(
			ctx,
			"Applying migration file",
			slog.String("filename", migration.name()),
		)
		rowsAffected, err := dbConn.applyMigration(ctx, dbConn.logger, tx, migration)
		if err != nil {
			dbConn.logger.ErrorContext(
				ctx,
				"Migration failed",
				slog.String("errorMessage", err.Error()),
			)
			rollback(ctx, tx, dbConn.logger, migration.name())
			errs = append(
				errs,
				fmt.Errorf("%w could not apply migration file: %s %w",
					ErrMigration,
					migration,
					err),
			)
			continue
		}

		fileToRowsAffected[migration.name()] = rowsAffected

		/* Log the migration to the database so we don't repeat it */
		dbConn.logger.DebugContext(
			ctx,
			fmt.Sprintf("Adding %s to the database", migration.name()),
		)
		_, err = tx.ExecContext(ctx, InsertMigrationStatement, migration.name())
		if err != nil {
			dbConn.logger.ErrorContext(
				ctx,
				"Error adding migration file to migrations table!",
				slog.String("filenam", migration.name()),
				slog.String("errorMessage", err.Error()),
			)
			errs = append(
				errs,
				fmt.Errorf(
					"%w: could not add migration file: %v to the list of migrations run",
					ErrMigration,
					migration.name(),
				),
			)
			break
		}

		err = tx.Commit()
		if err != nil {
			rollback(ctx, tx, dbConn.logger, migration.name())
			errs = append(
				errs,
				fmt.Errorf("error committing the migrations to the database: %w", err),
			)
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
			dbConn.logger.ErrorContext(
				ctx,
				"Error building metric on the rows updated by migration script",
				slog.String("migrationFile", key),
				slog.String("errorMessage", err.Error()),
			)
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
	tx *sql.Tx,
	migration migrationFile,
) (int64, error) {

	var totalRowsAffected int64 = 0
	sqlBytes, err := migration.readFile()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Error reading data from migration file",
			slog.String("migrationFile", migration.name()),
			slog.String("errorMessage", err.Error()),
		)
		return 0, fmt.Errorf("error reading migration data: %s", err.Error())
	}

	rawMigration := string(sqlBytes)

	if len(strings.TrimSpace(rawMigration)) < 1 {
		return 0, fmt.Errorf("empty migration file")
	}
	dbConn.logger.DebugContext(
		ctx,
		"SQL COMMAND",
		slog.String("statement", string(sqlBytes)),
	)

	result, err := tx.ExecContext(ctx, rawMigration)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Error applying migration",
			slog.String("sqlStatement", rawMigration),
			slog.String("errorMessage", err.Error()),
		)
		return 0, fmt.Errorf("error applying migration statement \"%s\": %s", rawMigration, err.Error())
	}

	if rowsAffected, err := result.RowsAffected(); err != nil {
		logger.ErrorContext(
			ctx,
			"Error getting the number of rows impacted",
			slog.String("errorMessage", err.Error()),
		)
		return 0, fmt.Errorf("error getting the number of rows impacted by sql statement \"%s\": %s", rawMigration, err.Error())
	} else {
		totalRowsAffected += rowsAffected
	}

	logger.DebugContext(
		ctx,
		"Migration applied",
		slog.Int64("rowsAffected", totalRowsAffected),
		slog.String("filename", migration.name()),
		slog.String("statement", rawMigration),
	)
	return totalRowsAffected, nil
}

// Create the migrations table if this is a fresh file
func (dbConn DBConn) createMigrationsTable(ctx context.Context) error {
	_, err := dbConn.Execute(ctx, "CREATE TABLE migrations (id INTEGER PRIMARY KEY AUTOINCREMENT, filename VARCHAR (255), applied_on TIMESTAMP)")
	return err
}

func listDataMigrations(migrationsDir fs.FS, root string) ([]migrationFile, error) {

	sqlFiles := []migrationFile{}

	migrationFiles, err := fs.ReadDir(migrationsDir, root)
	if err != nil {
		return sqlFiles, fmt.Errorf("error building the list of data migration files: %v", err)
	}

	for _, entry := range migrationFiles {

		/* Skip sub-directories */
		if entry.IsDir() {
			continue
		}

		sqlFiles = append(sqlFiles, dataMigrationFile{fs: migrationsDir, filename: entry.Name()})

	}

	return sqlFiles, nil

}

func listSchemaMigrations(migrationsDir embed.FS) ([]migrationFile, error) {

	sqlFiles := []migrationFile{}

	migrationFiles, err := migrationsDir.ReadDir("migrations")
	if err != nil {
		return sqlFiles, fmt.Errorf("error building the list of schema migration files: %v", err)
	}

	for _, entry := range migrationFiles {

		if entry.IsDir() {
			continue
		}

		sqlFiles = append(sqlFiles, schemaMigrationFile{fs: migrationsDir, filename: entry.Name()})

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
		logger.ErrorContext(
			ctx,
			"Error rolling migration query!",
			slog.String("migrationFile", migrationFilename),
			slog.String("errorMessage", err.Error()),
		)
		/* Panicing here because the database may well be in an invalid state */
		panic(err)
	}
}

func sortDirEntries(left migrationFile, right migrationFile) int {
	return strings.Compare(left.name(), right.name())
}

func (dmf dataMigrationFile) name() string {
	return dmf.filename
}

func (dmf dataMigrationFile) readFile() ([]byte, error) {

	return fs.ReadFile(dmf.fs, dmf.filename)

}

func (smf schemaMigrationFile) name() string {
	return smf.filename
}

func (smf schemaMigrationFile) readFile() ([]byte, error) {

	return smf.fs.ReadFile("migrations/" + smf.filename)

}
