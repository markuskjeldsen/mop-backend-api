package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

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

type AdvoProCaseData struct {
	Sagsnr       uint
	Status       int
	StatusText   string
	DeadlineDate time.Time
	KlientNavn   string
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func toTime(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case []byte:
		s := string(t)
		tt, _ := time.Parse(time.RFC3339, s)
		return tt
	case string:
		tt, _ := time.Parse(time.RFC3339, t)
		return tt
	default:
		return time.Time{}
	}
}

func GetConnection(server, database string) (*sql.DB, error) {
	user := os.Getenv("MSSQL_USER")
	pass := os.Getenv("MSSQL_PASS")

	connStr := fmt.Sprintf(
		"server=%s;user id=%s;password=%s;database=%s;encrypt=true;TrustServerCertificate=true;port=1433;connection timeout=5",
		server, user, pass, database,
	)

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection to %s/%s: %w", server, database, err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping %s/%s: %w", server, database, err)
	}

	return db, nil
}

func ExecuteQuery(server, database, query string, params ...interface{}) ([]map[string]interface{}, error) {
	user := os.Getenv("MSSQL_USER")
	pass := os.Getenv("MSSQL_PASS")

	// Option A: ODBC-style
	conn := fmt.Sprintf(
		"server=%s;user id=%s;password=%s;database=%s;encrypt=true;TrustServerCertificate=true;port=1433;connection timeout=5",
		server, user, pass, database,
	)

	// Option B: URL-style
	// conn := fmt.Sprintf("sqlserver://%s:%s@%s:1433?database=%s&encrypt=true&TrustServerCertificate=true",
	// 	 url.QueryEscape(user), url.QueryEscape(pass), server, database)

	db, err := sql.Open("sqlserver", conn)
	if err != nil {
		fmt.Print(err.Error())
		return nil, fmt.Errorf("server could not be opened: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(query, params...)
	if err != nil {
		fmt.Println(query)
		fmt.Println(params...)
		fmt.Print(err.Error())
		return nil, fmt.Errorf("Query could not be executed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		fmt.Print(err.Error())
		return nil, fmt.Errorf("failed to fetch column names from database: %w", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range cols {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			fmt.Println("Somthing went wrong in the data parsing")
			return nil, err
		}

		row := map[string]interface{}{}
		for i, c := range cols {
			row[c] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func FetchBulkCaseData(sagsnumre []uint) (map[uint]AdvoProCaseData, error) {
	if len(sagsnumre) == 0 {
		return nil, nil
	}

	// Create placeholders for the IN clause (@p1, @p2, @p3)
	placeholders := make([]string, len(sagsnumre))
	args := make([]interface{}, len(sagsnumre))
	for i, id := range sagsnumre {
		placeholders[i] = fmt.Sprintf("@p%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
        SELECT 
            f.Sagsnr,
            f.Status,
            f.Fristdato,
            s.KlientNavn,
            inks.Tekst
        FROM vwInkassoForlob f
        JOIN vwInkassoSag s ON s.Sagsnr = f.Sagsnr
        JOIN vwInkassoStatus inkS ON inkS.Statuskode = f.Status
        WHERE f.Sagsnr IN (%s)`, strings.Join(placeholders, ","))

	results, err := ExecuteQuery(Server, AdvoPro, query, args...)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	// TODO: WHAT IF 2 HAVE THE SAME SAGSNR?

	// Map results by Sagsnr for easy lookup
	caseMap := make(map[uint]AdvoProCaseData)
	for _, res := range results {
		sagsnr := uint(res["Sagsnr"].(int64))
		caseMap[sagsnr] = AdvoProCaseData{
			Sagsnr:       sagsnr,
			Status:       int(res["Status"].(int64)),
			StatusText:   res["Tekst"].(string),
			DeadlineDate: res["Fristdato"].(time.Time),
			KlientNavn:   res["KlientNavn"].(string),
		}
	}

	return caseMap, nil
}

func FetchDebitorData(debitorNum int64) *models.Debitor {
	debitors, err := ExecuteQuery(Server, AdvoPro, debitorQuery, debitorNum)
	if err != nil {
		fmt.Println("Something went wrong during the fetch of a debitor")
		fmt.Println(err)
		return nil
	}
	if len(debitors) > 1 {
		fmt.Println("There is more than one Debitor with this debitorID")
		return nil
	}
	if len(debitors) == 0 {
		fmt.Println("There is not any debitor with this ID")
		return nil
	}
	debitor := debitors[0]

	name, ok1 := debitor["Navn"].(string)
	birthday, ok2 := debitor["Fodselsdato"].(time.Time)
	genderNum, ok3 := debitor["Kon"].(int)
	phone, ok4 := debitor["Telefon"].(string)
	mobilePhone, ok5 := debitor["Mobiltlf"].(string)
	workPhone, ok6 := debitor["Arbejdstlf"].(string)
	email, ok7 := debitor["EPost"].(string)

	if !ok1 && !ok2 && !ok3 && !ok4 && !ok5 && !ok6 && !ok7 {
		fmt.Println("Formatting from the database has gone wrong")
		return nil
	}

	var phoneNr string
	if mobilePhone != "" {
		phoneNr = mobilePhone
	} else if phone != "" {
		phoneNr = phone
	} else if workPhone != "" {
		phoneNr = workPhone
	}

	var gender models.Gender
	switch genderNum {
	case 0:
		gender = models.Male
	case 1:
		gender = models.Female
	default:
		gender = models.Other
	}

	deb := models.Debitor{
		AdvoproDebitorId: int(debitorNum),
		Name:             name,
		Birthday:         birthday,
		Gender:           gender,
		Email:            email,
		Phone:            phoneNr,
		PhoneWork:        workPhone,
	}

	return &deb
}

type DebtRow struct {
	SumIndbetalinger      float64
	RestgeldAntaget       float64
	RestanceDato          time.Time // use appropriate type, e.g. time.Time or string
	KreditorHovedstol     float64
	RestgeldVedBrev       float64
	SumIndbetalingVedBrev float64
	Fordringsbeskrivelser string
	Sagsfremstillinger    string
}

func CurrentDebtCase(sagsnr uint) ([]DebtRow, error) {
	// 1. Error handling with context
	debts, err := ExecuteQuery(Server, AdvoPro, debtInfo, sagsnr)
	if err != nil {
		return nil, fmt.Errorf("failed to execute debt query for sagsnr %d: %w", sagsnr, err)
	}

	// 2. Pre-allocate slice capacity to avoid re-allocations in the loop
	result := make([]DebtRow, 0, len(debts))

	for _, debt := range debts {
		// 3. Use a helper to safely extract values and prevent panics
		row := DebtRow{
			SumIndbetalinger:      safeByteToFloat(debt["SumIndbetalinger"]),
			RestgeldAntaget:       safeByteToFloat(debt["restgeldAntaget"]),
			RestanceDato:          toTime(debt["RestanceDato"]),
			KreditorHovedstol:     safeByteToFloat(debt["KreditorHovedstol"]),
			RestgeldVedBrev:       safeByteToFloat(debt["restgeldVedBrev"]),
			SumIndbetalingVedBrev: safeByteToFloat(debt["SumIndbetalingVedBrev"]),
			Fordringsbeskrivelser: toString(debt["Fordringsbeskrivelser"]),
			Sagsfremstillinger:    toString(debt["Sagsfremstillinger"]),
		}

		result = append(result, row)
	}

	return result, nil
}

// safeByteToFloat prevents panics if the value is nil or not []byte
func safeByteToFloat(val interface{}) float64 {
	b, ok := val.([]byte)
	if !ok || b == nil {
		return 0.0
	}
	return byteToFloat(b)
}

// Converts []byte to float64, returns 0.0 on error
func byteToFloat(b []byte) float64 {
	s := string(b)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0 // or handle error as needed
	}
	return f
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

// ─── Insert Historik ──────────────────────────────────────────────────────────

// InsertHistorik inserts a new historik/note row into KlientHistorik.
// Returns the new HistorikId, or an error on failure.
// If DryRun is true, the transaction is rolled back and the would-be ID is returned.
func InsertHistorik(db *sql.DB, p InsertHistorikParams) (int, error) {
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
        VALUES (@sagsnr, @tidspunkt, @notattype, @tekst, @noter,
            @medarbejdernr, @skjulEksternt, 0, @oprettet, @oprettetAf,
            '', 0, 0, 0, 0, 0)
    `

	dryRunPrefix := ""
	if p.DryRun {
		dryRunPrefix = "[DRY RUN] "
	}
	log.Printf("%sInserting historik for Sagsnr %d", dryRunPrefix, p.Sagsnr)
	log.Printf("%sTekst: %s", dryRunPrefix, p.Tekst)

	// Begin transaction for manual control
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure rollback on panic or early return
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
		_ = tx.Rollback()
		tx = nil
		return 0, fmt.Errorf("failed to insert historik: %w", err)
	}

	if p.DryRun {
		log.Printf("[DRY RUN] Would have inserted HistorikId %d. Rolling back.", newID)
		_ = tx.Rollback()
		tx = nil
		return newID, nil
	}

	if err := tx.Commit(); err != nil {
		tx = nil
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	tx = nil // Prevent deferred rollback after successful commit
	log.Printf("Inserted HistorikId %d. Committed.", newID)

	return newID, nil
}

/*
def insert_historik(
    conn: pyodbc.Connection,
    sagsnr: int,
    tekst: str,
    noter: str,
    medarbejdernr: int,
    oprettet_af: str,
    notattype: int = 0,
    skjul_eksternt: bool = False,
    tidspunkt: Optional[datetime] = None,
    dry_run: bool = True
) -> Optional[int]:
    """
    Insert a new historik/note row.
    Returns the new HistorikId, or None on failure.
    """
    try:
        now = datetime.now()
        tidspunkt = tidspunkt or now

        sql = """
            INSERT INTO KlientHistorik (
                Sagsnr, Tidspunkt, Notattype, Tekst, Noter,
                Medarbejdernr, SkjulEksternt, JobId, Oprettet, OprettetAf,
                SlettetAf, HistorikType, HistorikRefId, Oprindelse, OprindelseId, DebitorAccepteret
            )
            OUTPUT INSERTED.HistorikId
            VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, '', 0, 0, 0, 0, 0)
        """
        values = (sagsnr, tidspunkt, notattype, tekst, noter,
                  medarbejdernr, int(skjul_eksternt), now, oprettet_af)

        logger.info(f"{'[DRY RUN] ' if dry_run else ''}Inserting historik for Sagsnr {sagsnr}")
        logger.info(f"{'[DRY RUN] ' if dry_run else ''}Tekst: {tekst}")

        cursor = conn.cursor()
        cursor.execute(sql, values)
        new_id = cursor.fetchone()[0]

        if dry_run:
            logger.info(f"[DRY RUN] Would have inserted HistorikId {new_id}. Rolling back.")
            conn.rollback()
            return new_id  # Return what would have been the ID
        else:
            conn.commit()
            logger.info(f"Inserted HistorikId {new_id}. Committed.")
            return new_id

    except Exception as e:
        logger.error(f"Error during insert: {e}")
        conn.rollback()
        return None


		# ─── Connection ───────────────────────────────────────────────────────────────

def get_connection(server: str, database: str, trusted_connection: bool = True) -> pyodbc.Connection:
    conn_str = (
        f"DRIVER={{ODBC Driver 17 for SQL Server}};"
        f"SERVER={server};"
        f"DATABASE={database};"
        f"Trusted_Connection={'yes' if trusted_connection else 'no'};"
    )
    conn = pyodbc.connect(conn_str)
    conn.autocommit = False  # Always manual transaction control
    return conn


# ─── Data Classes ─────────────────────────────────────────────────────────────

@dataclass
class Historik:
    HistorikId: int
    Sagsnr: int
    Tidspunkt: Optional[datetime]
    Notattype: int
    Tekst: str
    Noter: Optional[str]
    Medarbejdernr: int
    SkjulEksternt: bool
    JobId: int
    Oprettet: datetime
    OprettetAf: str
    Slettet: Optional[datetime]
    SlettetAf: str
    HistorikType: int
    HistorikRefId: int
    Oprindelse: int
    OprindelseId: int
    DebitorAccepteret: bool



*/
