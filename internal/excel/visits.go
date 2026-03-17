package excel

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/models" // Adjust to your actual module name
	"github.com/xuri/excelize/v2"
)

// GenerateVisitsExcel creates the Excel file structure based on a slice of Visits
func GenerateVisitsExcel(visits []models.Visit) (*excelize.File, error) {
	f := excelize.NewFile()
	sheetName := "Sheet1"

	// TODO: kig på

	// Excel Header
	// 					ID 		Sagsnr
	// stop, title, Address, Service Time, Arrival Time, Distance,
	// Comment 1 (debitor navne),
	// Comment 2 (status kode),
	// Comment 3 (Status tekst),
	// Comment 4 (Frist Dato),
	// Comment 5 (klient),
	// Comment 6 (besøgsid)
	// Konsulent(navn),
	// Dato(for besøg),
	// Interval
	// "Debitors",

	headers := []string{
		"Title", "Address", "Service Time", "Comment 1",
		"Comment 2", "Comment 3", "Comment 4", "Comment 5", "Comment 6",
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
	}

	replacer := strings.NewReplacer("\n", " ", "\r", " ")

	for rowIdx, visit := range visits {
		var debitorNames []string
		for _, d := range visit.Debitors {
			debitorNames = append(debitorNames, replacer.Replace(d.Name))
		}

		data := []interface{}{
			visit.Sagsnr,
			replacer.Replace(visit.Address),
			"15",
			strings.Join(debitorNames, ", "),
			fmt.Sprintf("%d", visit.AdvoproStatus),
			visit.AdvoproStatusText,
			visit.AdvoproDeadlineDate,
			visit.AdvoproKlient,
			fmt.Sprintf("%d", visit.ID),
		}

		for colIdx, value := range data {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue(sheetName, cell, value)
		}
	}

	return f, nil
}

// SendExcelResponse handles the Gin-specific response headers and buffer writing
func SendExcelResponse(c *gin.Context, f *excelize.File, filename string) {
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		c.JSON(500, gin.H{"error": "Failed to generate Excel file"})
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}
