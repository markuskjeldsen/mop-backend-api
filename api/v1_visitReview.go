package api

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
)

// Regex to split on "v/" or "c/o" (with optional spaces/dots, e.g., "v/", "v. ", "c/o")
var vSplitRegex = regexp.MustCompile(`(?i)\s+(?:v/|v\.|c/o)\s+`)

// Regex to remove invalid filename/title characters and collapse multiple spaces/dashes
var invalidCharsRegex = regexp.MustCompile(`[\\/*?:"<>|]`)
var multiDashRegex = regexp.MustCompile(`-+`)

func sanitizeAndDeduplicateNames(rawNames []string) []string {
	var result []string
	seen := make(map[string]bool)

	for _, raw := range rawNames {
		// 1. Split "Happy horse v/ peter petersen" into ["Happy horse", "peter petersen"]
		parts := vSplitRegex.Split(raw, -1)

		for _, part := range parts {
			// 2. Remove illegal characters like / \ : * ? " < > |
			cleaned := invalidCharsRegex.ReplaceAllString(part, "")

			// 3. Normalize spaces and trim
			cleaned = strings.TrimSpace(cleaned)

			if cleaned == "" {
				continue
			}

			// 4. Deduplicate (case-insensitive check)
			key := strings.ToLower(cleaned)
			if !seen[key] {
				seen[key] = true
				result = append(result, cleaned)
			}
		}
	}

	return result
}

func VisitPDF(c *gin.Context) {

	visitID, err := strconv.ParseInt(c.Query("id"), 10, 32)
	if err != nil {
		logger.Errorf("VisitPDF: failed to parse id: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "The id could not be parsed",
			"err":   err.Error(),
			"id":    visitID,
		})
		return
	}
	// does the actual visit have a response?
	var visitcheck models.Visit
	initializers.DB.Preload("VisitResponse").First(&visitcheck, visitID)
	if visitcheck.VisitResponse == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "The given visit does not have a visitresponse",
			"id":    visitID,
		})
		return
	}

	pdfBytes, err := internal.GeneratePDFVisit(uint(visitID))
	if err != nil {
		logger.Errorf("VisitPDF: PDF generation failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
			"id":    visitID,
		})
		return
	}
	/*
		ok := internal.AddNoteToAdvopro(visitcheck)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Advopro integration went wrong",
				"id":    visitcheck.Sagsnr,
			})
			return
		}
	*/
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

// savePDFTemp writes pdfBytes to a temp file and returns its path.
// Caller is responsible for removing it.
func savePDFTemp(pdfBytes []byte, name string) (string, error) {
	f, err := os.CreateTemp("", "visit-"+name+"-*.pdf")
	// → visit-20260611-430415-1234567890.pdf
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(pdfBytes); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	return f.Name(), nil
}

// Upload to Advopro
func ReviewedVisit(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, nil)
		return
	}

	var body struct {
		ReviewedIds []uint `json:"reviewed_ids"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	type iErr struct {
		Err string `json:"err"`
		ID  uint   `json:"id"`
	}

	var iErrs []iErr

	for _, visitId := range body.ReviewedIds {
		item := iErr{ID: visitId, Err: "no error"}

		// 1. Get sagsnr
		var visit models.Visit
		if res := initializers.DB.Preload("Debitors").Preload("VisitResponse").First(&visit, visitId); res.Error != nil {
			item.Err = res.Error.Error()
			iErrs = append(iErrs, item)
			continue
		}

		// 1.5 generate a name
		if visit.VisitResponse == nil {
			item.Err = "Visit has no visitresponse"
			iErrs = append(iErrs, item)
			continue
		}

		// Collect raw names
		rawNames := make([]string, len(visit.Debitors))
		for i, d := range visit.Debitors {
			rawNames[i] = d.Name
		}

		// Clean, split on "v/", deduplicate, and sanitize
		sanitizedNames := sanitizeAndDeduplicateNames(rawNames)

		// Join names with a dash or space
		namePart := strings.Join(sanitizedNames, "-")
		// Optional: replace spaces with dashes or keep them as you prefer
		// namePart = strings.ReplaceAll(namePart, " ", "_")

		docTitle := "besog " + visit.VisitResponse.ActDate.Format("2006-01-02") + "-" +
			strconv.FormatUint(uint64(visit.Sagsnr), 10) + "-" + namePart

		// 2. Generate PDF
		pdfBytes, err := internal.GeneratePDFVisit(visitId)
		if err != nil {
			item.Err = err.Error()
			iErrs = append(iErrs, item)
			continue
		}

		// 3. Save to temp file
		path, err := savePDFTemp(pdfBytes, docTitle)
		if err != nil {
			item.Err = err.Error()
			iErrs = append(iErrs, item)
			continue
		}

		// 4. Upload document
		if err := internal.UploadDocument(path, uint64(visit.Sagsnr), docTitle); err != nil {
			os.Remove(path)
			item.Err = err.Error()
			iErrs = append(iErrs, item)
			continue
		}
		os.Remove(path) // clean up temp file after successful upload

		// 5. Only NOW update the status, since the document is on the case
		if err := internal.UpdateVisitStatus(visitId, 5, user.ID); err != nil {
			item.Err = err.Error()
			iErrs = append(iErrs, item)
			continue
		}

		iErrs = append(iErrs, item)
	}
	// If every item has an error, return 500
	allFailed := len(iErrs) > 0
	for _, e := range iErrs {
		if e.Err == "no error" {
			allFailed = false
			break
		}
	}
	var errStrings []string
	for _, err := range iErrs {
		errStrings = append(errStrings, err.Err)
	}
	ErrString := strings.Join(errStrings, ", ")

	if allFailed {
		logger.Errorf("Every review failed %s", ErrString)
		c.JSON(http.StatusInternalServerError, iErrs)
		return
	}

	logger.Errorf("some review failed %s", ErrString)
	c.JSON(http.StatusOK, iErrs)
}
