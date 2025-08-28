package internal

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func ExecuteQuery(server, database string, query string, params ...interface{}) ([]map[string]interface{}, error) {
	// Connection string: user/password uses SQL Auth; for integrated (Windows) auth, see below
	connString := fmt.Sprintf("server=%s;database=%s;trusted_connection=yes", server, database)

	// Open connection
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Pass parameters to db.Query()
	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Getting columns dynamically
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	results := []map[string]interface{}{}
	for rows.Next() {
		// Create a slice of interfaces to represent each column
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		err = rows.Scan(valuePtrs...)
		if err != nil {
			return nil, err
		}

		row := map[string]interface{}{}
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	return results, nil
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
