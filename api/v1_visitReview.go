package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func VisitPDF(c *gin.Context) {

	visitID, err := strconv.ParseInt(c.Query("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "The id could not be parsed",
			"err":   err.Error(),
			"id":    visitID,
		})
		return
	}

	pdfBytes := internal.GeneratePDFVisit(uint(visitID))
	var visit models.Visit
	initializers.DB.First(&visit, visitID)
	filename := "id" + strconv.Itoa(int(visit.ID)) + "_sagsnr" + strconv.Itoa(int(visit.Sagsnr)) + ".pdf"

	// Set headers for PDF download
	c.Header("Access-Control-Expose-Headers", "Content-Disposition")
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Length", fmt.Sprintf("%d", len(pdfBytes)))

	// Send PDF bytes
	c.Data(http.StatusOK, "application/pdf", pdfBytes)
}
