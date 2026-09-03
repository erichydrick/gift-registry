package registry

import (
	"database/sql"
	"gift-registry/internal/middleware"
	"gift-registry/internal/util"
	"html/template"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ItemRow struct {
	personID       int64
	personExtID    string
	personDispName string
	personLastName string

	itemExtID        sql.NullString
	itemName         sql.NullString
	itemQty          sql.NullInt16
	itemURL          sql.NullString
	itemNotes        sql.NullString
	claimedHousehold sql.NullString
	claimedQty       sql.NullInt16
	claimType        sql.NullString
	giftDate         sql.NullTime
}

type Registries struct {
	ErrorMessage string
	Registries   iter.Seq[RegistryPerson]
}

type RegistryPerson struct {
	PersonID    string
	DisplayName string
	LastName    string

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
	GiftDate     time.Time
	Type         string
}

const (
	selectItemsForRegistriesQuery = `	
		SELECT person.person_id,
			person.external_id, 
			person.display_name, 
			person.last_name, 
			item.external_id, 
			item.name, 
			item.quantity, 
			item.url, 
			item.notes, 
			household.name, 
			claim.quantity, 
			claim.claim_type, 
			claim.gift_date 
		FROM people person
			LEFT OUTER JOIN items item ON person.person_id = item.gift_for 
			LEFT OUTER JOIN item_claims claim ON item.item_id = claim.item_id 
			LEFT OUTER JOIN households household ON claim.household_id = household.household_id 
			ORDER BY person.person_id ASC,
				item.item_id ASC
	`
)

// RegistryHandler returns the registry items, grouped by person, for
// bulk display in the UI.
func RegistryHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		span := trace.SpanFromContext(ctx)
		span.SetName("registry_handler")

		/*
			We want to track the difference between the REQUESTED quantity and the
			CLAIMED quantity, but to do that we need a subtraction function we can
			pass in.
		*/
		funcMap := template.FuncMap{
			"subtract": func(requested int8, claimed int8) int8 {
				return requested - claimed
			},
		}

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
			res.Write([]byte("Error rendering the profile page"))
			span.SetAttributes(attribute.String("error_message", err.Error()))
			return
		}

		results, err := svr.DB.Query(
			ctx,
			selectItemsForRegistriesQuery,
		)
		if err != nil {
			svr.Logger.ErrorContext(
				ctx,
				"Error looking up gift registries for all users",
				slog.String("errorMessage", err.Error()),
				slog.String("query", selectItemsForRegistriesQuery),
			)
		}

		registries := Registries{}
		curUser := middleware.PersonID(res, req)
		people := map[string]RegistryPerson{}

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
				&rawRowData.claimType,
				&rawRowData.giftDate,
			)
			if err != nil {
				svr.Logger.ErrorContext(
					ctx,
					"Error reading DB row. Skipping...",
					slog.Int("resultNum", cnt),
					slog.String("errorMessage", err.Error()),
					slog.String("query", selectItemsForRegistriesQuery),
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
				people[person.PersonID] = person

			}

			person.addItem(rawRowData, curUser)
			cnt++

		}

		res.WriteHeader(200)
		for person := range maps.Values(people) {
			svr.Logger.DebugContext(
				ctx,
				"Registry ready for rendering",
				slog.Any("person", person),
			)
		}
		registries.Registries = maps.Values(people)

		err = tmpl.ExecuteTemplate(res, "registry-page", registries)
		if err != nil {
			errorMessage := err.Error()
			svr.Logger.ErrorContext(
				ctx,
				"Error writing template!",
				slog.String("errorMessage", errorMessage),
			)
			res.WriteHeader(500)
			res.Write([]byte("Error loading registry page"))
			span.SetAttributes(attribute.String("error_message", errorMessage))
			return
		}

	})

}

func createPerson(rowData ItemRow) RegistryPerson {

	return RegistryPerson{
		PersonID:    rowData.personExtID,
		DisplayName: rowData.personDispName,
		LastName:    rowData.personLastName,

		Items: map[string]RegistryItem{},
	}

}

func (person *RegistryPerson) addItem(rowData ItemRow, currentUser int64) {

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

	/*
		This is an unclaimed item, go ahead and return.
	*/
	if !rowData.claimedHousehold.Valid {

		person.Items[item.ItemID] = item
		return

	}

	claim := RegistryItemClaim{
		Claimant:     rowData.claimedHousehold.String,
		ClaimedCount: int8(rowData.claimedQty.Int16),
		GiftDate:     rowData.giftDate.Time,
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

	/*
		Don't show the user who's getting their upcoming gifts!
	*/
	if currentUser == rowData.personID {
		claim.Claimant = "???"
	}

	item.Claims = append(item.Claims, claim)

	person.Items[item.ItemID] = item

}
