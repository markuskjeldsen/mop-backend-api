package initializers

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

const Server = "MOPSRV01\\SQL1"
const AdvoPro = "AdvoPro"

const StatusFemQuery = `
SELECT
	f.Sagsnr as sagsnr,
	f.Status as status,
	f.ForlobInfo as forlobInfo,
	d.Navn as navn,
	d.Adresse as adresse,
	d.Postnr as postnr,
	d.Bynavn as bynavn,
	d.Noter as noter,
	d.DebitorId as debitorId
FROM
	vwInkassoForlob f
JOIN
	vwInkassoForlobDebitor fd ON fd.ForlobId = f.ForlobId
JOIN
	vwInkassoDebitor d ON d.DebitorId = fd.DebitorId
WHERE
	f.Status = 5
order by f.Sagsnr`

const SagsnrQuery = `
SELECT 
    F.Sagsnr as sagsnr,
    D.Navn as navn,
    D.Adresse as adresse
FROM
    vwInkassoForlob F
JOIN
    vwInkassoForlobDebitor FD on FD.ForlobId = F.ForlobId
JOIN
    vwInkassoDebitor D ON D.DebitorId = FD.DebitorId
WHERE
    F.Sagsnr = @p1
`
const debitorQuery = `
SELECT
	*
from
	vwInkassoDebitor d
where
	d.DebitorId = @p1
`

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
	if genderNum == 0 {
		gender = models.Male
	} else if genderNum == 1 {
		gender = models.Female
	} else {
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
