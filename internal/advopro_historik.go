package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/markuskjeldsen/mop-backend-api/models"
)

// ─── Data Structure ───────────────────────────────────────────────────────────

type Historik struct {
	HistorikId        int
	Sagsnr            int
	Tidspunkt         *time.Time
	Notattype         int
	Tekst             string
	Noter             *string
	Medarbejdernr     int
	SkjulEksternt     bool
	JobId             int
	Oprettet          time.Time
	OprettetAf        string
	Slettet           *time.Time
	SlettetAf         string
	HistorikType      int
	HistorikRefId     int
	Oprindelse        int
	OprindelseId      int
	DebitorAccepteret bool
}

type InsertHistorikParams struct {
	Sagsnr        int
	Tekst         string
	Noter         string
	Medarbejdernr int
	OprettetAf    string
	Notattype     int        // default: 0
	SkjulEksternt bool       // default: false
	Tidspunkt     *time.Time // optional, defaults to now
	DryRun        bool       // default: true
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// openDB opens a new *sql.DB using env credentials.
func openDB(server, database string) (*sql.DB, error) {
	user := os.Getenv("MSSQL_USER")
	pass := os.Getenv("MSSQL_PASS")

	connStr := fmt.Sprintf(
		"server=%s;user id=%s;password=%s;database=%s;encrypt=true;TrustServerCertificate=true;port=1433;connection timeout=5",
		server, user, pass, database,
	)

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("server could not be opened: %w", err)
	}
	return db, nil
}

// mapToHistorik converts a raw result row into a Historik struct.
func mapToHistorik(row map[string]interface{}) Historik {
	parseInt := func(v interface{}) int {
		switch val := v.(type) {
		case int64:
			return int(val)
		case int32:
			return int(val)
		case int:
			return val
		}
		return 0
	}

	parseString := func(v interface{}) string {
		if v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}

	parseTime := func(v interface{}) *time.Time {
		if v == nil {
			return nil
		}
		if t, ok := v.(time.Time); ok {
			return &t
		}
		return nil
	}

	parseStringPtr := func(v interface{}) *string {
		if v == nil {
			return nil
		}
		if s, ok := v.(string); ok {
			return &s
		}
		return nil
	}

	oprettet := time.Time{}
	if t := parseTime(row["Oprettet"]); t != nil {
		oprettet = *t
	}

	return Historik{
		HistorikId:        parseInt(row["HistorikId"]),
		Sagsnr:            parseInt(row["Sagsnr"]),
		Tidspunkt:         parseTime(row["Tidspunkt"]),
		Notattype:         parseInt(row["Notattype"]),
		Tekst:             parseString(row["Tekst"]),
		Noter:             parseStringPtr(row["Noter"]),
		Medarbejdernr:     parseInt(row["Medarbejdernr"]),
		SkjulEksternt:     parseInt(row["SkjulEksternt"]) != 0,
		JobId:             parseInt(row["JobId"]),
		Oprettet:          oprettet,
		OprettetAf:        parseString(row["OprettetAf"]),
		Slettet:           parseTime(row["Slettet"]),
		SlettetAf:         parseString(row["SlettetAf"]),
		HistorikType:      parseInt(row["HistorikType"]),
		HistorikRefId:     parseInt(row["HistorikRefId"]),
		Oprindelse:        parseInt(row["Oprindelse"]),
		OprindelseId:      parseInt(row["OprindelseId"]),
		DebitorAccepteret: parseInt(row["DebitorAccepteret"]) != 0,
	}
}

// ─── Query Functions ──────────────────────────────────────────────────────────

// QueryHistorik returns all historik entries for a given Sagsnr.
// Set includeDeleted to true to also return soft-deleted rows.
func QueryHistorik(sagsnr int, includeDeleted bool) ([]Historik, error) {
	query := `
        SELECT HistorikId, Sagsnr, Tidspunkt, Notattype, Tekst, Noter,
               Medarbejdernr, SkjulEksternt, JobId, Oprettet, OprettetAf,
               Slettet, SlettetAf, HistorikType, HistorikRefId,
               Oprindelse, OprindelseId, DebitorAccepteret
        FROM KlientHistorik
        WHERE Sagsnr = @sagsnr`

	if !includeDeleted {
		query += " AND Slettet IS NULL"
	}
	query += " ORDER BY HistorikId DESC"

	rows, err := ExecuteQuery(Server, AdvoPro, query, sql.Named("sagsnr", sagsnr))
	if err != nil {
		return nil, fmt.Errorf("QueryHistorik failed: %w", err)
	}

	results := make([]Historik, 0, len(rows))
	for _, row := range rows {
		results = append(results, mapToHistorik(row))
	}

	log.Printf("Found %d historik entries for Sagsnr %d", len(results), sagsnr)
	return results, nil
}

// GetHistorikById fetches a single historik entry by its ID.
// Returns nil if not found.
func GetHistorikById(historikId int) (*Historik, error) {
	query := `
        SELECT HistorikId, Sagsnr, Tidspunkt, Notattype, Tekst, Noter,
               Medarbejdernr, SkjulEksternt, JobId, Oprettet, OprettetAf,
               Slettet, SlettetAf, HistorikType, HistorikRefId,
               Oprindelse, OprindelseId, DebitorAccepteret
        FROM KlientHistorik
        WHERE HistorikId = @historikId`

	rows, err := ExecuteQuery(Server, AdvoPro, query, sql.Named("historikId", historikId))
	if err != nil {
		return nil, fmt.Errorf("GetHistorikById failed: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	h := mapToHistorik(rows[0])
	return &h, nil
}

// ─── Insert ───────────────────────────────────────────────────────────────────

// InsertHistorik inserts a new historik/note row into KlientHistorik.
// Returns the new HistorikId or an error.
// If DryRun is true, the transaction is rolled back and the would-be ID is returned.
func InsertHistorik(p InsertHistorikParams) (int, error) {
	now := time.Now()

	tidspunkt := now
	if p.Tidspunkt != nil {
		tidspunkt = *p.Tidspunkt
	}

	skjulEksterntInt := 0
	if p.SkjulEksternt {
		skjulEksterntInt = 1
	}

	query := `
        INSERT INTO KlientHistorik (
            Sagsnr, Tidspunkt, Notattype, Tekst, Noter,
            Medarbejdernr, SkjulEksternt, JobId, Oprettet, OprettetAf,
            SlettetAf, HistorikType, HistorikRefId, Oprindelse, OprindelseId, DebitorAccepteret
        )
        OUTPUT INSERTED.HistorikId
        VALUES (
            @sagsnr, @tidspunkt, @notattype, @tekst, @noter,
            @medarbejdernr, @skjulEksternt, 0, @oprettet, @oprettetAf,
            '', 0, 0, 0, 0, 0
        )`

	dryRunPrefix := ""
	if p.DryRun {
		dryRunPrefix = "[DRY RUN] "
	}
	log.Printf("%sInserting historik for Sagsnr %d", dryRunPrefix, p.Sagsnr)
	log.Printf("%sTekst: %s", dryRunPrefix, p.Tekst)

	db, err := openDB(Server, AdvoPro)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var newID int
	err = tx.QueryRow(
		query,
		sql.Named("sagsnr", p.Sagsnr),
		sql.Named("tidspunkt", tidspunkt),
		sql.Named("notattype", p.Notattype),
		sql.Named("tekst", p.Tekst),
		sql.Named("noter", p.Noter),
		sql.Named("medarbejdernr", p.Medarbejdernr),
		sql.Named("skjulEksternt", skjulEksterntInt),
		sql.Named("oprettet", now),
		sql.Named("oprettetAf", p.OprettetAf),
	).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert historik: %w", err)
	}

	if p.DryRun {
		log.Printf("[DRY RUN] Would have inserted HistorikId %d. Rolling back.", newID)
		_ = tx.Rollback()
		tx = nil
		return newID, nil
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil

	log.Printf("Inserted HistorikId %d. Committed.", newID)
	return newID, nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

// UpdateHistorikParams holds the fields allowed to be updated.
// Only non-nil fields will be applied to the UPDATE statement.
type UpdateHistorikParams struct {
	Tekst             *string
	Noter             *string
	Notattype         *int
	Tidspunkt         *time.Time
	Medarbejdernr     *int
	SkjulEksternt     *bool
	HistorikType      *int
	DebitorAccepteret *bool
}

// UpdateHistorik safely updates a historik row.
// expectedTekst is a safety check - the update aborts if it doesn't match the current value.
// Returns true if the update was applied (or would be in dry run mode).
func UpdateHistorik(historikId int, expectedTekst string, updates UpdateHistorikParams, dryRun bool) (bool, error) {
	current, err := GetHistorikById(historikId)
	if err != nil {
		return false, fmt.Errorf("failed to fetch historik: %w", err)
	}
	if current == nil {
		return false, fmt.Errorf("HistorikId %d not found", historikId)
	}
	if current.Tekst != expectedTekst {
		return false, fmt.Errorf(
			"safety check failed: expected Tekst %q but found %q",
			expectedTekst, current.Tekst,
		)
	}
	if current.Slettet != nil {
		return false, fmt.Errorf("HistorikId %d is already deleted, aborting", historikId)
	}

	// Build SET clause dynamically from non-nil fields
	setClauses := []string{}
	params := []interface{}{}

	if updates.Tekst != nil {
		setClauses = append(setClauses, "Tekst = @tekst")
		params = append(params, sql.Named("tekst", *updates.Tekst))
	}
	if updates.Noter != nil {
		setClauses = append(setClauses, "Noter = @noter")
		params = append(params, sql.Named("noter", *updates.Noter))
	}
	if updates.Notattype != nil {
		setClauses = append(setClauses, "Notattype = @notattype")
		params = append(params, sql.Named("notattype", *updates.Notattype))
	}
	if updates.Tidspunkt != nil {
		setClauses = append(setClauses, "Tidspunkt = @tidspunkt")
		params = append(params, sql.Named("tidspunkt", *updates.Tidspunkt))
	}
	if updates.Medarbejdernr != nil {
		setClauses = append(setClauses, "Medarbejdernr = @medarbejdernr")
		params = append(params, sql.Named("medarbejdernr", *updates.Medarbejdernr))
	}
	if updates.SkjulEksternt != nil {
		v := 0
		if *updates.SkjulEksternt {
			v = 1
		}
		setClauses = append(setClauses, "SkjulEksternt = @skjulEksternt")
		params = append(params, sql.Named("skjulEksternt", v))
	}
	if updates.HistorikType != nil {
		setClauses = append(setClauses, "HistorikType = @historikType")
		params = append(params, sql.Named("historikType", *updates.HistorikType))
	}
	if updates.DebitorAccepteret != nil {
		v := 0
		if *updates.DebitorAccepteret {
			v = 1
		}
		setClauses = append(setClauses, "DebitorAccepteret = @debitorAccepteret")
		params = append(params, sql.Named("debitorAccepteret", v))
	}

	if len(setClauses) == 0 {
		return false, fmt.Errorf("no fields provided to update")
	}

	// Build final query
	setStr := ""
	for i, c := range setClauses {
		if i > 0 {
			setStr += ", "
		}
		setStr += c
	}
	query := fmt.Sprintf("UPDATE KlientHistorik SET %s WHERE HistorikId = @historikId", setStr)
	params = append(params, sql.Named("historikId", historikId))

	dryRunPrefix := ""
	if dryRun {
		dryRunPrefix = "[DRY RUN] "
	}
	log.Printf("%sExecuting: %s", dryRunPrefix, query)
	log.Printf("%sParams: %v", dryRunPrefix, params)

	db, err := openDB(Server, AdvoPro)
	if err != nil {
		return false, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.Exec(query, params...)
	if err != nil {
		return false, fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	if dryRun {
		log.Printf("[DRY RUN] Would have updated %d row(s). Rolling back.", rowsAffected)
		_ = tx.Rollback()
		tx = nil
		return true, nil
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil

	log.Printf("Updated %d row(s). Committed.", rowsAffected)
	return true, nil
}

// ─── Delete (Soft) ────────────────────────────────────────────────────────────

// DeleteHistorik soft-deletes a historik entry by setting Slettet and SlettetAf.
func DeleteHistorik(historikId int, expectedTekst, deletedBy string, dryRun bool) (bool, error) {
	current, err := GetHistorikById(historikId)
	if err != nil {
		return false, fmt.Errorf("failed to fetch historik: %w", err)
	}
	if current == nil {
		return false, fmt.Errorf("HistorikId %d not found", historikId)
	}
	if current.Tekst != expectedTekst {
		return false, fmt.Errorf(
			"safety check failed: expected Tekst %q but found %q",
			expectedTekst, current.Tekst,
		)
	}
	if current.Slettet != nil {
		log.Printf("Warning: HistorikId %d is already deleted", historikId)
		return false, nil
	}

	query := `
        UPDATE KlientHistorik
        SET Slettet = @slettet, SlettetAf = @slettetAf
        WHERE HistorikId = @historikId`

	dryRunPrefix := ""
	if dryRun {
		dryRunPrefix = "[DRY RUN] "
	}
	log.Printf("%sSoft-deleting HistorikId %d", dryRunPrefix, historikId)

	db, err := openDB(Server, AdvoPro)
	if err != nil {
		return false, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.Exec(
		query,
		sql.Named("slettet", time.Now()),
		sql.Named("slettetAf", deletedBy),
		sql.Named("historikId", historikId),
	)
	if err != nil {
		return false, fmt.Errorf("delete failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	if dryRun {
		log.Printf("[DRY RUN] Would have deleted HistorikId %d (%d row(s)). Rolling back.", historikId, rowsAffected)
		_ = tx.Rollback()
		tx = nil
		return true, nil
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil

	log.Printf("Soft-deleted HistorikId %d. Committed.", historikId)
	return true, nil
}

func AddNoteToAdvopro(visit models.Visit) bool {
	note := ""

	if *visit.VisitResponse.DebitorIsHome {
		note = note + "The debitor is home"
	} else {
		note = note + "The debitor was not home"
	}
	// TODO: add more fields.

	// then we write to advopro database
	// --- Insert (dry run) ---
	newID, err := InsertHistorik(InsertHistorikParams{
		Sagsnr:        int(visit.Sagsnr),
		Tekst:         "Besøgs notat",
		Noter:         note,
		Medarbejdernr: 185,
		OprettetAf:    `MOP\mkk`,
		DryRun:        true, // flip to false when ready
	})
	if err != nil {
		log.Fatalf("Insert failed: %v", err)
	}
	fmt.Printf("Would have inserted HistorikId: %d\n", newID)

	return true
}

func UpdateBehandlingskodeText(additionalText string) bool {
	// UPDATE KlientBehandlingSag
	//SET Tekst = Tekst + ' Modtaget konsulentrapport pr. 26.03.2026:
	//Socialt boligbyggeri af nyere dato. Dør telefon med postkasser udenfor. Ingen svarer ved dørtelefon. Begge køretøjer er på adressen.
	//Andet navn på dørtelefon og postkasse. Nabo bekræfter, at ham der kører taxaen, bor på adressen, men kender ikke navnet.
	//Køretøjet er mere værd end restgælden, men sagen må sendes frem, da debitor ikke er til at komme i kontakt med'
	//WHERE BehandlingId = 183741

	text := `you can update the text in the behandlingskode
			but remember there can only be 270 chars in there. 
			and it probably already says 'Besøg kl. 13:00 - 16:00' `
	// 270 - 23 = 257 chars
	// we will use a note because it allows for more text
	// i think its unlimited, but should be resonable.
	// propably the comments/ short review of the visit will be there

	fmt.Println(text)
	return true
}

/*
func main() {
    server := "MOPSRV01\\SQL1"
    database := "AdvoPro"
    sagsnr := 430415

    // --- Query all historik ---
    entries, err := QueryHistorik(server, database, sagsnr, false)
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }
    for _, h := range entries {
        fmt.Printf("HistorikId=%d Tekst=%q\n", h.HistorikId, h.Tekst)
    }

    // --- Insert (dry run) ---
    newID, err := InsertHistorik(server, database, InsertHistorikParams{
        Sagsnr:        sagsnr,
        Tekst:         "Besøgs notat",
        Noter:         "Notat indhold her...",
        Medarbejdernr: 185,
        OprettetAf:    `MOP\mkk`,
        DryRun:        true, // flip to false when ready
    })
    if err != nil {
        log.Fatalf("Insert failed: %v", err)
    }
    fmt.Printf("Would have inserted HistorikId: %d\n", newID)

    // --- Update (dry run) ---
    tekst := "Updated tekst"
    ok, err := UpdateHistorik(server, database, 2321092, "Old tekst", UpdateHistorikParams{
        Tekst: &tekst,
    }, true)
    if err != nil {
        log.Fatalf("Update failed: %v", err)
    }
    fmt.Printf("Update applied: %v\n", ok)

    // --- Soft delete (dry run) ---
    ok, err = DeleteHistorik(server, database, 2321092, "Old tekst", `MOP\mkk`, true)
    if err != nil {
        log.Fatalf("Delete failed: %v", err)
    }
    fmt.Printf("Delete applied: %v\n", ok)
}


*/
