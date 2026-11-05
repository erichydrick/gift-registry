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
	"gift-registry/internal/templates"
	"gift-registry/internal/util"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type itemRequest struct {
	ErrorMsg        string
	ItemID          string
	NewItemName     string
	NewItemNotes    string
	newItemSQLNotes sql.NullString
	newItemSQLURL   sql.NullString
	NewItemQuantity int
	NewItemURL      string
	PersonID        string
}

type itemRow struct {
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

type registries struct {
	ErrorMsg  string
	Wishlists []registryPerson
}

type registryPerson struct {
	dbID            int64
	DisplayName     string
	Editable        bool
	ErrorMsg        string
	LastName        string
	NewItemName     string
	NewItemQuantity int
	NewItemURL      string
	NewItemNotes    string
	PersonID        string

	Items map[string]registryItem
}

type registryItem struct {
	ErrorMsg     string
	ItemID       string
	Name         string
	Quantity     int8
	URL          string
	Notes        string
	Claims       []registryItemClaim
	TotalClaimed int8
}

type registryItemClaim struct {
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
		VALUES (?,
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
		%s
		ORDER BY person.person_id ASC,
			item.item_id ASC
	`

	filterByPersonClause = `WHERE person.person_id = ?`

	/*
		Querying by external ID since it's possible to add an item for someone
		other than yourself
	*/
	selectPersonID = `
		SELECT person.person_id
		FROM people person
		WHERE person.external_id = ? 
	`
)

func AddItemHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("add_item")

		funcMap := mapRegistryFuncs(ctx, svr, false)

		personID := middleware.PersonID(req)
		household := middleware.HouseholdID(req)
		giftFor := req.PathValue("externalID")

		requested, err := buildRequestedItem(req, giftFor)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error building item request",
				slog.String("itemName", req.FormValue("itemName")),
				slog.String("itemQuantity", req.FormValue("itemQty")),
				slog.String("errorMessage", err.Error()),
				slog.Any("requested", requested),
			)
			tmplErr := templates.WriteResponseTemplate(
				res,
				400,
				"new-item-form",
				[]string{"templates/new_item_form.html"},
				requested,
				funcMap,
			)
			if tmplErr != nil {
				errorMessage := fmt.Errorf(
					"Could not write error response: %v - caused by: %v",
					tmplErr,
					err,
				).Error()
				svr.Logger.ErrorContext(
					ctx,
					"Error writing item back for editing!",
					slog.String("errorMessage", errorMessage),
				)
				span.SetAttributes(attribute.String("error_message", errorMessage))
			}
			return
		}

		/* Lookup the ID for the recipient so we can set up the relation properly */
		var recipientID int64
		err = svr.DB.QueryRow(
			ctx,
			selectPersonID,
			giftFor,
		).Scan(&recipientID)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error looking up the recipient for the requested item",
				slog.String("giftFor", giftFor),
				slog.String("errorMessage", err.Error()),
			)
			requested.ErrorMsg = "Sorry, we had some technical difficulties. Please reload the page and try again."
			tmplErr := templates.WriteResponseTemplate(
				res,
				500,
				"new-item-form",
				[]string{"templates/new_item_form.html"},
				requested,
				funcMap,
			)
			if tmplErr != nil {
				errorMessage := fmt.Errorf(
					"could not find recipient: %v - caused by: %v",
					tmplErr,
					err,
				).Error()
				svr.Logger.ErrorContext(
					ctx,
					"Error writing item back for editing!",
					slog.String("errorMessage", errorMessage),
				)
				span.SetAttributes(attribute.String("error_message", errorMessage))
			}
			return
		}

		result, err := svr.DB.Execute(
			ctx,
			insertNewItemStatement,
			recipientID,
			personID,
			personID,
			requested.ItemID,
			requested.NewItemName,
			requested.NewItemQuantity,
			requested.newItemSQLURL,
			requested.newItemSQLNotes,
		)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error saving the requested item to the database",
				slog.String("giftFor", giftFor),
				slog.String("itemName", requested.NewItemName),
				slog.Int("itemQty", requested.NewItemQuantity),
				slog.String("itemURL", requested.NewItemURL),
				slog.String("itemNotes", requested.NewItemNotes),
				slog.String("errorMessage", err.Error()),
			)
			tmplErr := templates.WriteResponseTemplate(
				res,
				500,
				"new-item-form",
				[]string{"templates/new_item_form.html"},
				requested,
				funcMap,
			)
			if tmplErr != nil {
				errorMessage := fmt.Errorf(
					"could not write save new item failure response: %v - caused by %v",
					tmplErr,
					err,
				).Error()
				svr.Logger.ErrorContext(
					ctx,
					"Error writing item back for editing!",
					slog.String("errorMessage", errorMessage),
				)
				span.SetAttributes(attribute.String("error_message", errorMessage))
			}
			return
		} else if rowsAdded, err := result.RowsAffected(); err == nil {
			span.SetAttributes(attribute.Int64("item_rows_added", rowsAdded))
		}

		svr.Logger.InfoContext(
			ctx,
			"Saved the item to the database, reloading the wishlist.",
			slog.String("giftFor", giftFor),
			slog.String("itemRequested", requested.NewItemName),
			slog.Int("numRequested", requested.NewItemQuantity),
		)

		wishlist, err := buildRegistries(
			ctx,
			svr,
			false,
			recipientID,
			personID,
			household,
		)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Could not pull latest person-specific gift registry data!",
				slog.String("errorMessage", err.Error()),
				slog.String("giftFor", giftFor),
			)
		}

		res.WriteHeader(200)
		svr.Logger.DebugContext(
			ctx,
			"WRITING DATA BACK",
			// slog.Any("wishlist", wishlist.Wishlists[0]),
			slog.Any("items", wishlist.Wishlists[0].Items),
		)
		err = templates.WriteResponseTemplate(
			res,
			200,
			"registry-table",
			[]string{
				"templates/registry_table.html",
				"templates/new_item_form.html",
			},
			wishlist.Wishlists[0],
			funcMap,
		)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing item back for editing!",
				slog.String("errorMessage", errorMessage),
			)
			span.SetAttributes(attribute.String("error_message", errorMessage))
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

		funcMap := mapRegistryFuncs(ctx, svr, historicalParam == "true")

		curUser := middleware.PersonID(req)
		household := middleware.HouseholdID(req)

		registries, err := buildRegistries(
			ctx,
			svr,
			historicalParam == "true",
			0,
			curUser,
			household,
		)

		err = templates.WriteResponsePage(
			res,
			200,
			"registry-page",
			[]string{
				"templates/new_item_form.html",
				"templates/registry_page.html",
				"templates/registry_table.html",
			},
			registries,
			funcMap,
		)

		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", errorMessage),
			)
			span.SetAttributes(attribute.String("error_message", errorMessage))
		}
	})
}

func createPerson(rowData itemRow) registryPerson {
	return registryPerson{
		dbID:        rowData.personID,
		PersonID:    rowData.personExtID,
		DisplayName: rowData.personDispName,
		LastName:    rowData.personLastName,

		Items: map[string]registryItem{},
	}
}

func addClaim(
	ctx context.Context,
	svr *util.ServerUtils,
	rowData itemRow,
	item *registryItem) (registryItemClaim, time.Time) {

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

	claim := registryItemClaim{
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

func (person *registryPerson) addItem(
	ctx context.Context,
	svr *util.ServerUtils,
	rowData itemRow,
	currentUser int64,
	now time.Time) {

	/*
		This person hasn't requested anything yet, move on.
	*/
	if !rowData.itemExtID.Valid || rowData.itemExtID.String == "" {
		return
	}

	item, ok := person.Items[rowData.itemExtID.String]

	/* We haven't seen this item yet. */
	if rowData.itemExtID.Valid && !ok {
		item = registryItem{
			ItemID:   rowData.itemExtID.String,
			Name:     rowData.itemName.String,
			Quantity: int8(rowData.itemQty.Int16),
			URL:      rowData.itemURL.String,
			Notes:    rowData.itemNotes.String,
			Claims:   []registryItemClaim{},
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

func buildRegistries(
	ctx context.Context,
	svr *util.ServerUtils,
	historical bool,
	registryOwner int64,
	curUser int64,
	household int64,
) (registries, error) {

	comparator := ">="
	nullCheck := "IS NULL OR"

	/*
		Change the query from future gifts to past gifts if and ONLY if the
		historical query paramater is present and set to "true" (I'm not parsing
		the query parameter as if it could have multiple values, because we're ONLY
		showing historical data if and ONLY if the historical flag is exactly "true"
	*/
	if historical {

		comparator = "<"
		nullCheck = "IS NOT NULL AND"

	}

	ownerFilter := ""
	params := []any{}
	if registryOwner > 0 {

		ownerFilter = filterByPersonClause
		params = append(params, registryOwner)

	}

	/*
		A person is "editable" if they're the current active user or it's someone managed
		by this user.
	*/
	selfAndMangedPeople := findEditablePeople(
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
		ownerFilter,
	)
	results, err := svr.DB.Query(
		ctx,
		query,
		params...,
	)
	if err != nil {
		svr.Logger.ErrorContext(
			ctx,
			"Error looking up gift registries for all users",
			slog.String("errorMessage", err.Error()),
			slog.String("query", query),
		)
		return registries{}, fmt.Errorf("error building wishlist: %v", err)
	}

	registries := registries{}
	people := map[string]registryPerson{}

	now := time.Now()

	cnt := 1
	for results.Next() {

		var rawRowData itemRow

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
			registries.ErrorMsg = "Error reading some of the registry data"
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

	return registries, nil

}

func buildRequestedItem(req *http.Request, giftFor string) (requested itemRequest, err error) {

	/*
		Default the item with just the name to start, then we'll parse the form
		data and fill in as appropriate
	*/
	requested = itemRequest{
		ItemID:      uuid.New().String(),
		NewItemName: req.FormValue("itemName"),
		newItemSQLNotes: sql.NullString{
			Valid:  false,
			String: "",
		},
		NewItemQuantity: 0,
		newItemSQLURL: sql.NullString{
			Valid:  false,
			String: "",
		},
		PersonID: giftFor,
	}

	if quantity, convErr := strconv.Atoi(req.FormValue("itemQty")); err == nil {

		requested.NewItemQuantity = quantity

	} else {

		err = fmt.Errorf("invalid quantity (%v): %v",
			req.FormValue("itemQuantity"), convErr)

	}

	/*
		Notes and URLs are optional, which means they're nuallable in the database.
		sql.NullString let's us preserve that without awkward mappings.
	*/
	if notes := req.FormValue("itemNotes"); notes != "" {

		requested.newItemSQLNotes = sql.NullString{
			Valid:  true,
			String: notes,
		}
		requested.NewItemNotes = notes

	}

	if url := req.FormValue("itemURL"); url != "" {

		requested.newItemSQLURL = sql.NullString{
			Valid:  true,
			String: url,
		}
		requested.NewItemURL = url

	}

	/*
		The onnly way err != nil at this point is if someone typed a non-number
		into the quantity field, and that has a more descriptive error message
		already set.
	*/
	if (requested.NewItemName == "" || requested.NewItemQuantity < 1) && err == nil {

		err = fmt.Errorf("items have to have a name and quantity")
		requested.ErrorMsg = "Please make sure you have both a name and a quantity when asking for something"

	}

	return requested, err

}

func findEditablePeople(
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

func mapRegistryFuncs(
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
