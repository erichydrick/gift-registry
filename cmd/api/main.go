package main

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"log/slog"
	"os"
	"strconv"
)

type person struct {
	ExternalId   string `db:"external_id"`
	FirstName    string `db:"first_name"`
	LastName     string `db:"last_name"`
	EmailAddress string `db:"email_address"`
}

func (p person) String() string {

	return fmt.Sprintf("{ExternalId: %s, FirstName: %s, LastName: %s, EmailAddress: %s}", p.ExternalId, p.FirstName, p.LastName, p.EmailAddress)

}

func main() {

	/*
	   Configure logging
	*/
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewJSONHandler(os.Stderr, options)
	logger := slog.New(handler)

	dbPort, err := strconv.Atoi(os.Getenv("DB_PORT"))
	if err != nil {
		logger.Error("Could not convert DB_PORT value into an integer", slog.String("portEnv", os.Getenv("DB_PORT")), slog.String("error", err.Error()))
		panic(err)
	}

	dbConn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", os.Getenv("DB_HOST"),
		dbPort, os.Getenv("DB_USER"), os.Getenv("DB_PASS"), os.Getenv("DB_NAME"))

	db, err := sql.Open("postgres", dbConn)
	if err != nil {
		logger.Error("Could not open database connection", slog.String("error", err.Error()))
		panic(err)
	}
	defer db.Close()

	query := "SELECT external_id, first_name, last_name, email_address FROM person"
	rows, err := db.Query(query)
	if err != nil {
		logger.Error("Error reading from the database", slog.String("query", query), slog.String("error", err.Error()))
		return
	}
	defer rows.Close()

	logger.Info("Got results, need to read them")
	rowNum := 1
	for rows.Next() {

		individual := person{}
		err := rows.Scan(&individual.ExternalId, &individual.FirstName, &individual.LastName, &individual.EmailAddress)
		if err != nil {
			logger.Error("Error reading query result row", slog.Int("rowNum", rowNum), slog.String("error", err.Error()))
		}

		rowNum++
		logger.Info("Read the following individual from the DB", slog.String("recordData", fmt.Sprintf("%s", individual)))
	}

	logger.Info("Done reading data from rows")
}
