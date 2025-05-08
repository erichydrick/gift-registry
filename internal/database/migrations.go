package database

import (
	"database/sql"
	"embed"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
)

//go:embed migrations
var dbMigrations embed.FS

// Checks for any pending database migrations and applies them
/* TODO: ADD CONTEXT SO I CAN CANCEL DB MIGRATIONS IF THE SERVER IS SHUT DOWN */
func RunMigrations(db *sql.DB, logger *slog.Logger) error {

	// TODO:
	// 3. FOR EACH FILE:
	// 3A. LOAD THE CONTENTS
	// 3B. EXECUTE THE QUERY
	// 3C. CAPTURE THE ASSOCIATED METRICS AND TRACE ATTRIBUTES
	// 4. LOG THE OBSERVABILITY DATA

	migrationFiles, err := dbMigrations.ReadDir(".")
	if err != nil {
		logger.Error("Error reading database migration file list", slog.String("errorMessage", err.Error()))
	}

	slices.SortFunc(migrationFiles, sortDirEntries)
	logger.Debug("Migration files", slog.Any("migrationFiles", migrationFiles))
	return nil

}

func sortDirEntries(left fs.DirEntry, right fs.DirEntry) int {

	/*
		Directories should be listed first
	*/
	switch {
	case left.IsDir() && !right.IsDir():
		return -1
	case !left.IsDir() && right.IsDir():
		return 1
	default:
		return strings.Compare(left.Name(), right.Name())
	}

}
