package api

import (
	"database/sql"
	"fmt"

	_ "github.com/denisenkom/go-mssqldb"
)

const SagsnrQuery = `
SELECT 
    F.Sagsnr,
    D.Navn,
    D.Adresse
FROM
    vwInkassoForlob F
JOIN
    vwInkassoForlobDebitor FD on FD.ForlobId = F.ForlobId
JOIN
    vwInkassoDebitor D ON D.DebitorId = FD.DebitorId
WHERE
    F.Sagsnr = @p1
`

func ExecuteQuery(server, database string, params ...interface{}) ([]map[string]interface{}, error) {
	// Connection string: user/password uses SQL Auth; for integrated (Windows) auth, see below
	connString := fmt.Sprintf("server=%s;database=%s;trusted_connection=yes", server, database)

	// Open connection
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Pass parameters to db.Query()
	rows, err := db.Query(SagsnrQuery, params...)
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
