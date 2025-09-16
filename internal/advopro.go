package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

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
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range cols {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
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
}

func CurrentDebtCase(sagsnr uint) []DebtRow {
	debts, err := ExecuteQuery(Server, AdvoPro, debtInfo, sagsnr)
	if err != nil {
		log.Fatalln(err.Error())
	}

	var result []DebtRow
	for _, debt := range debts {
		row := DebtRow{
			SumIndbetalinger:      byteToFloat(debt["SumIndbetalinger"].([]byte)),
			RestgeldAntaget:       byteToFloat(debt["restgeldAntaget"].([]byte)),
			RestanceDato:          debt["RestanceDato"].(time.Time), // adjust type if needed!
			KreditorHovedstol:     byteToFloat(debt["KreditorHovedstol"].([]byte)),
			RestgeldVedBrev:       byteToFloat(debt["restgeldVedBrev"].([]byte)),
			SumIndbetalingVedBrev: byteToFloat(debt["SumIndbetalingVedBrev"].([]byte)),
		}

		result = append(result, row)
	}

	return result
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
