package registry

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gift-registry/internal/middleware"
	"gift-registry/internal/util"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type item struct {
	externalID string
	name       string
	quantity   int
	url        sql.NullString
	notes      sql.NullString
}

type ItemRow struct {
	personID       int64
	personExtID    string
	personDispName string
	personLastName string

	itemExtID sql.NullString
	itemName  sql.NullString
	itemNotes sql.NullString
	itemQty   sql.NullInt16
	itemURL   sql.NullString

	claimedHousehold sql.NullString
	claimedQty       sql.NullInt16
	claimNotes       sql.NullString
	claimType        sql.NullString
	giftDate         sql.NullString
}

type Registries struct {
	ErrorMessage string
	Wishlists    []RegistryPerson
}

type RegistryPerson struct {
	dbID         int64
	DisplayName  string
	Editable     bool
	ErrorMsg     string
	LastName     string
	NewItemName  string
	NewItemQty   int
	NewItemURL   string
	NewItemNotes string
	PersonID     string

	Items map[string]RegistryItem
}

type RegistryItem struct {
	ItemID       string
	Name         string
	Quantity     int8
	URL          string
	Notes        string
	Claims       []RegistryItemClaim
	TotalClaimed int8
}

type RegistryItemClaim struct {
	Claimant     string
	ClaimedCount int8
	Notes        string
	GiftDate     string
	Type         string
}

const (
	insertNewItemStatement = `
		INSERT INTO items (gift_for, 
			added_by,
			last_updated_by,
			external_id,
			name,
			quantity,
			url,
			notes,
			added_on,
			last_updated_on
		)
		VALUES ((SELECT person_id FROM people WHERE external_id = ?),
			?,
			?,
			?,
			?,
			?,
			?,
			?,
			Datetime('now'),
			Datetime('now')
		)
	`

	selectEditableWishtlists = `
		SELECT person.person_id
		FROM people person
			INNER JOIN household_people hp ON hp.person_id = person.person_id
		WHERE person.person_id = ? 
			OR (hp.household_id = ? AND person.type = 'MANAGED')
	`

	selectItemsForRegistriesQuery = `
		WITH gift_items AS (SELECT item.item_id,
				item.gift_for,
				item.external_id,
				item.name,
				item.quantity,
				item.url,
				item.notes AS item_notes,
				claim.household_id,
				claim.quantity AS claim_quantity,
				claim.notes AS claim_notes,	
				claim.claim_type,
				claim.gift_date
			FROM items item
				LEFT OUTER JOIN item_claims claim ON item.item_id = claim.item_id
			WHERE (claim.gift_date %s claim.gift_date %s Datetime('now')))
		SELECT person.person_id,
			person.external_id, 
			person.display_name, 
			person.last_name, 
			item.external_id, 
			item.name, 
			item.quantity, 
			item.url, 
			item.item_notes, 
			household.name, 
			item.claim_quantity, 
			item.claim_notes,
			item.claim_type, 
			item.gift_date 
		FROM people person
			LEFT OUTER JOIN gift_items item ON person.person_id = item.gift_for 
			LEFT OUTER JOIN households household ON item.household_id = household.household_id 
		ORDER BY person.person_id ASC,
			item.item_id ASC
	`

	/*
		Querying by external ID since it's possible to add an item for someone
		other than yourself
	*/
	selectPersonDetails = `
		SELECT person.person_id,
			person.external_id, 
			person.display_name,
			person.last_name
		FROM people person
		WHERE person.external_id = ? 
	`

	selectPersonalWishlist = `
		SELECT item.external_id,
			item.name,
			item.quantity,
			item.url,
			item.notes AS item_notes,
			claim.household_id,
			claim.quantity AS claim_quantity,
			claim.notes AS claim_notes,	
			claim.claim_type,
			claim.gift_date
		FROM items item
			LEFT OUTER JOIN item_claims claim ON item.item_id = claim.item_id
		WHERE item.gift_for = ? 
			AND (claim.gift_date IS NULL OR claim.gift_date >= Datetime('now')) 
	`
)

func AddItemHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("add_item")

		funcMap := registryFuncMap(ctx, svr, false)

		templatesDir := svr.Getenv("TEMPLATES_DIR")
		tmpl, err := template.New("registry_template").
			Funcs(funcMap).
			ParseFiles(templatesDir + "/registry_table.html")
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading registry template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading the registry page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		personID := middleware.PersonID(req)
		giftFor := req.PathValue("externalID")

		externalID := uuid.New()
		quantity, err := strconv.Atoi(req.FormValue("quantity"))
		if err != nil {
			svr.Logger.WarnContext(
				ctx,
				"Could not read quanity from the form data, defaulting to 1",
				slog.String("formValue", req.FormValue("quantity")),
				slog.String("errorMessage", err.Error()),
			)
			quantity = 1
		}

		/* URL and notes are optional, so they're nullable in the DB */
		link := sql.NullString{
			String: req.FormValue("url"),
			Valid:  req.FormValue("url") != "",
		}

		notes := sql.NullString{
			String: req.FormValue("notes"),
			Valid:  req.FormValue("notes") != "",
		}

		newItem := item{
			externalID: externalID.String(),
			name:       req.FormValue("name"),
			quantity:   quantity,
			url:        link,
			notes:      notes,
		}

		span.SetAttributes(
			attribute.String("external_id", newItem.externalID),
			attribute.String("name", newItem.name),
			attribute.Int("quantity", newItem.quantity),
		)

		if newItem.url.Valid {

			span.SetAttributes(attribute.String("url", newItem.url.String))

		}

		if newItem.notes.Valid {

			span.SetAttributes(attribute.String("notes", newItem.notes.String))

		}

		person := RegistryPerson{
			Editable:     true,
			Items:        map[string]RegistryItem{},
			NewItemName:  newItem.name,
			NewItemQty:   newItem.quantity,
			NewItemURL:   newItem.url.String,
			NewItemNotes: newItem.notes.String,
		}

		result, err := svr.DB.Execute(
			ctx,
			insertNewItemStatement,
			giftFor,
			personID,
			personID,
			externalID,
			newItem.name,
			newItem.quantity,
			newItem.url,
			newItem.notes,
		)

		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Could not save new item",
				slog.String("name", newItem.name),
				slog.Int("quantity", newItem.quantity),
				slog.String("errorMessage", err.Error()),
			)
			person.updateErrorMessage("Sorry, we couldn't save that for some reason.")
		}

		if rowCnt, err := result.RowsAffected(); err != nil {
			svr.Logger.WarnContext(
				ctx,
				"Could not get the rows affected from the insert. Did it not stick?",
				slog.String("errorMessage", err.Error()),
			)
		} else {
			span.SetAttributes(attribute.Int64("items_added", rowCnt))
		}

		err = svr.DB.QueryRow(
			ctx,
			selectPersonDetails,
			giftFor,
		).Scan(&person.dbID, &person.PersonID, &person.DisplayName, &person.LastName)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Couldn't look persnoal details",
				slog.String("requestor", giftFor),
				slog.String("errorMesssage", err.Error()),
			)

			person.updateErrorMessage("Sorry, we had a problem looking up your details.")

		}

		results, err := svr.DB.Query(
			ctx,
			selectPersonalWishlist,
			person.dbID,
		)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			err = tmpl.ExecuteTemplate(res, "registry-table", person)

			if err != nil {
				errorMessage := err.Error()
				svr.Logger.ErrorContext(
					ctx,
					"Error writing template!",
					slog.String("errorMessage", errorMessage),
				)
				res.WriteHeader(500)
				res.Write([]byte("Error rendering registry items"))
				span.SetAttributes(attribute.String("error_message", errorMessage))
				return
			}

		}

		now := time.Now()
		for results.Next() {

			var rowData ItemRow
			err = results.Scan(
				&rowData.itemExtID,
				&rowData.itemName,
				&rowData.itemQty,
				&rowData.itemURL,
				&rowData.itemNotes,
				&rowData.claimedHousehold,
				&rowData.claimedQty,
				&rowData.claimNotes,
				&rowData.claimType,
				&rowData.giftDate,
			)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error reading item information from the database",
					slog.String("errorMessage", err.Error()),
				)
				person.updateErrorMessage("Sorry, we had problem reading your wishlist.")
			}

			person.addItem(
				ctx,
				svr,
				rowData,
				personID,
				now,
			)

		}

		/*
			We successfully saved the item and re-loaded the wishlist. Clear the form
			for the next new item (when needed). Quantity still defaults to 1 because
			asking for 0 soemthing is stupid.
		*/
		person.NewItemName = ""
		person.NewItemQty = 1
		person.NewItemURL = ""
		person.NewItemNotes = ""

		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "registry-table", person)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", errorMessage),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering registry page"))
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}

	})

}

// RegistryHandler returns the registry items, grouped by person, for
// bulk display in the UI.
func RegistryHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("registry_handler")

		historicalParam := req.URL.Query().Get("historical")
		historicalParam = strings.ToLower(strings.TrimSpace(historicalParam))

		comparator := ">="
		nullCheck := "IS NULL OR"

		/*
			Change the query from future gifts to past gifts if and ONLY if the
			historical query paramater is present and set to "true" (I'm not parsing
			the query parameter as if it could have multiple values, because we're ONLY
			showing historical data if and ONLY if the historical flag is exactly "true"
		*/
		if historicalParam == "true" {

			comparator = "<"
			nullCheck = "IS NOT NULL AND"

		}

		funcMap := registryFuncMap(ctx, svr, historicalParam == "true")

		templatesDir := svr.Getenv("TEMPLATES_DIR")
		tmpl, err := template.New("registry_template").
			Funcs(funcMap).
			ParseFiles(templatesDir+"/registry_page.html", templatesDir+"/registry_table.html")
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error loading registry template",
				slog.String("errorMessage", err.Error()),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading the registry page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		curUser := middleware.PersonID(req)
		household := middleware.HouseholdID(req)

		/*
			A person is "editable" if they're the current active user or it's someone managed
			by this user.
		*/
		selfAndMangedPeople := editablePeople(
			ctx,
			svr,
			curUser,
			household,
		)

		/* TODO: THIS QUERY (AND PROCESSOR) CAN GO IN A GO FUNC WHILE WE GET MANAGED PEOPLE */
		/* Assigning to a variable so I can capture how the %s flags formatted */
		query := fmt.Sprintf(
			selectItemsForRegistriesQuery,
			nullCheck,
			comparator,
		)
		results, err := svr.DB.Query(
			ctx,
			query,
		)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error looking up gift registries for all users",
				slog.String("errorMessage", err.Error()),
				slog.String("query", query),
			)
		}

		registries := Registries{}
		people := map[string]RegistryPerson{}

		now := time.Now()

		cnt := 1
		for results.Next() {

			var rawRowData ItemRow

			err = results.Scan(
				&rawRowData.personID,
				&rawRowData.personExtID,
				&rawRowData.personDispName,
				&rawRowData.personLastName,
				&rawRowData.itemExtID,
				&rawRowData.itemName,
				&rawRowData.itemQty,
				&rawRowData.itemURL,
				&rawRowData.itemNotes,
				&rawRowData.claimedHousehold,
				&rawRowData.claimedQty,
				&rawRowData.claimNotes,
				&rawRowData.claimType,
				&rawRowData.giftDate,
			)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error reading DB row. Skipping...",
					slog.Int("resultNum", cnt),
					slog.String("errorMessage", err.Error()),
					slog.String("query", query),
				)
				registries.ErrorMessage = "Error reading some of the registry data"
				continue
			}

			person, ok := people[rawRowData.personExtID]

			/*
				We're on to a new registry. Create the struct for the person whose gift
				list we're looking at.

			*/
			if !ok {

				person = createPerson(rawRowData)
				person.Editable = slices.Contains(selfAndMangedPeople, person.dbID)
				people[person.PersonID] = person
				registries.Wishlists = append(registries.Wishlists, person)

			}

			person.addItem(ctx, svr, rawRowData, curUser, now)
			cnt++

		}

		res.WriteHeader(200)
		err = tmpl.ExecuteTemplate(res, "registry-page", registries)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", errorMessage),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error rendering registry page"))
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}
	})
}

func createPerson(rowData ItemRow) RegistryPerson {
	return RegistryPerson{
		dbID:        rowData.personID,
		PersonID:    rowData.personExtID,
		DisplayName: rowData.personDispName,
		LastName:    rowData.personLastName,

		Items: map[string]RegistryItem{},
	}
}

func addClaim(
	ctx context.Context,
	svr *util.ServerUtils,
	rowData ItemRow,
	item *RegistryItem) (RegistryItemClaim, time.Time) {

	giftDate, err := time.Parse(time.DateOnly, rowData.giftDate.String[0:10])
	if err != nil {
		svr.Logger.ErrorContext(
			ctx,
			"Could not parse gift date from database result, skipping.",
			slog.Bool("databaseDatePresent", rowData.giftDate.Valid),
			slog.String("databaseDate", rowData.giftDate.String),
			slog.String("errorMessage", err.Error()),
		)
	}

	claim := RegistryItemClaim{
		Claimant:     rowData.claimedHousehold.String,
		ClaimedCount: int8(rowData.claimedQty.Int16),
		GiftDate:     giftDate.Format(time.DateOnly),
		Type:         rowData.claimType.String,
	}

	/*
		The second (and beyond) joint claim should not impact the count that people
		have committed to getting. Only adjust the claimed count if nobody has
		already committed to getting it or 2+ separate people are making 2+ separate
		purchases (e.g. 2+ partial claims)
	*/
	if len(item.Claims) == 0 || (claim.Type != "" && claim.Type != "JOINT") {
		item.TotalClaimed += claim.ClaimedCount
	}

	return claim, giftDate

}

func (person *RegistryPerson) addItem(
	ctx context.Context,
	svr *util.ServerUtils,
	rowData ItemRow,
	currentUser int64,
	now time.Time) {

	/*
		This person hasn't requested anything yet, move on.
	*/
	if !rowData.itemExtID.Valid || rowData.itemExtID.String == "" {
		svr.Logger.ErrorContext(
			ctx,
			"No item information, skipping",
			slog.String("itemName", rowData.itemName.String),
		)
		return
	}

	item, ok := person.Items[rowData.itemExtID.String]

	/* We haven't seen this item yet. */
	if rowData.itemExtID.Valid && !ok {
		item = RegistryItem{
			ItemID:   rowData.itemExtID.String,
			Name:     rowData.itemName.String,
			Quantity: int8(rowData.itemQty.Int16),
			URL:      rowData.itemURL.String,
			Notes:    rowData.itemNotes.String,
			Claims:   []RegistryItemClaim{},
		}
	}

	/* This is an unclaimed item, go ahead and return. */
	if !rowData.claimedHousehold.Valid || !rowData.giftDate.Valid {

		person.Items[item.ItemID] = item
		return

	}

	claim, giftDate := addClaim(ctx, svr, rowData, &item)

	/*
		Don't show the user who's getting their upcoming gifts!
	*/
	if currentUser == rowData.personID && !giftDate.Before(now) {
		claim.Claimant = "???"
	}

	item.Claims = append(item.Claims, claim)
	person.Items[item.ItemID] = item

}

func editablePeople(
	ctx context.Context,
	svr *util.ServerUtils,
	curUser int64,
	household int64) []int64 {

	editableLists := []int64{}
	editableIDs, err := svr.DB.Query(ctx, selectEditableWishtlists, curUser, household)
	if err != nil {
		svr.Logger.ErrorContext(
			ctx,
			"Could not look up which wishlists this user can edit.",
			slog.String("errorMessage", err.Error()),
			slog.Int64("personID", curUser),
		)
	}

	for editableIDs.Next() {

		var id int64
		err = editableIDs.Scan(&id)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Could not read at least 1 of the IDs from the list of editable wishlists, skipping it.",
				slog.String("errorMessage", err.Error()),
			)
			continue
		}

		editableLists = append(editableLists, id)

	}

	return editableLists

}

func registryFuncMap(
	ctx context.Context,
	svr *util.ServerUtils,
	historicalParam bool,
) template.FuncMap {

	return template.FuncMap{
		"editable": func(editableLists []string, id string) bool {
			return slices.Contains(editableLists, id)
		},
		"isHistorical": func() bool {
			return historicalParam
		},
		"formatDate": func(datetime string) string {
			if parsed, err := time.Parse("2006-01-02", datetime); err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error converting date to locale string",
					slog.String("givenDatetime", datetime),
					slog.String("errorMessage", err.Error()),
				)
				return datetime
			} else {
				return parsed.Format("01/02/06")
			}
		},
		"max": func(leftNum int, rightNum int) int {
			return max(leftNum, rightNum)
		},
		"subtract": func(requested int8, claimed int8) int8 {
			return requested - claimed
		},
	}

}

func (person *RegistryPerson) updateErrorMessage(newMessage string) {

	/*
		We could have more than 1 error being returned.
		Just put each on its own line.
	*/
	if person.ErrorMsg != "" {

		person.ErrorMsg += "<br />"

	}

	person.ErrorMsg += newMessage

}
