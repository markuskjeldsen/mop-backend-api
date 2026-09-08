package internal

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/google/uuid"
)

// --- Transaction-aware query helper ---

// execInsertReturnID runs an INSERT ... SELECT SCOPE_IDENTITY() inside a transaction
// and returns the new identity value.
func execInsertReturnID(tx *sql.Tx, name, query string, params ...interface{}) (int64, error) {
	row := tx.QueryRow(query, params...)
	var id float64 // SCOPE_IDENTITY() returns numeric/decimal
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to retrieve %s: %w", name, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("failed to retrieve %s: got 0", name)
	}
	return int64(id), nil
}

// openAdvoPro opens a *sql.DB connection to the AdvoPro database.
func openAdvoPro() (*sql.DB, error) {
	user := os.Getenv("MSSQL_USER")
	pass := os.Getenv("MSSQL_PASS")
	conn := fmt.Sprintf(
		"server=%s;user id=%s;password=%s;database=%s;encrypt=disable;TrustServerCertificate=true;port=1433;connection timeout=15",
		Server, user, pass, AdvoPro,
	)
	db, err := sql.Open("sqlserver", conn)
	if err != nil {
		return nil, fmt.Errorf("server could not be opened: %w", err)
	}
	return db, nil
}

// --- Path resolution ---

// getCasePath fetches the file storage path from existing docs on this case.
// Returns the Windows-style Placering (no trailing backslash) or "" if none found.
func getCasePath(db *sql.DB, sagsnr uint64) (string, error) {
	query := `
        SELECT TOP 1 dv.Placering
        FROM DokumentVersion dv
        JOIN DokumentForsendelseDokument fd ON fd.DokumentVersionId = dv.DokumentVersionId
        JOIN DokumentForsendelse f ON f.ForsendelseId = fd.ForsendelseId
        WHERE f.Sagsnr = @p1 AND dv.Placering IS NOT NULL AND dv.Placering != ''
        ORDER BY dv.DokumentVersionId DESC`

	row := db.QueryRow(query, sagsnr)
	var placering string
	if err := row.Scan(&placering); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return strings.TrimRight(placering, "\\"), nil
}

// winPathToLocal translates a Windows AdvoPro path into the locally-accessible path.
// On Windows it returns the path unchanged; on Linux it maps the UNC prefix to the mount.
func winPathToLocal(winPath string) string {
	if runtime.GOOS == "windows" {
		return winPath
	}
	winPrefix := `\\MOPSRV01\AdvoPro`
	linuxMount := "/mnt/advopro"
	rel := strings.TrimPrefix(winPath, winPrefix)
	rel = strings.ReplaceAll(rel, "\\", "/")
	return filepath.Join(linuxMount, rel)
}

type ImportResult struct {
	DokID         int64
	VersionID     int64
	ForsendelseID int64
}

// ImportDocument mirrors the Python import_document function.
//   - srcFilePath: local path to the file to import
//   - title: document title shown in AdvoPro
//   - sagsnr: case number
//   - empID: Medarbejdernr (default 185 = MKK)
//   - user: created-by username
//   - destFolder: override storage path (Windows-style). If empty, auto-detect.
//   - dryRun: rolls back the transaction instead of committing.
func ImportDocument(srcFilePath, title string, sagsnr uint64, empID int, user, destFolder string, dryRun bool) (*ImportResult, error) {

	// NOTE: We deliberately use two separate short-lived DB connections here.
	// Phase 1 resolves pre-checks, Phase 2 handles the transaction.
	// This avoids holding a connection open during potentially slow file operations
	// (PDF generation, network share copy), which was causing i/o timeout on commit.

	const defaultEmpID = 185
	const defaultUser = "AUTO_IMPORT"
	if empID == 0 {
		empID = defaultEmpID
	}
	if user == "" {
		user = defaultUser
	}

	ext := filepath.Ext(srcFilePath)
	uniqueName := uuid.New().String() + ext

	// --- Phase 1: Pre-checks (short-lived connection) ---
	db1, err := openAdvoPro()
	if err != nil {
		return nil, err
	}

	// Idempotency guard
	exists, err := documentExists(db1, sagsnr, title)
	if err != nil {
		db1.Close()
		return nil, err
	}
	if exists {
		db1.Close()
		return nil, errors.New("document already exists on case")
	}

	// Resolve storage path
	if destFolder == "" {
		destFolder, err = getCasePath(db1, sagsnr)
		if err != nil {
			db1.Close()
			return nil, fmt.Errorf("failed to resolve case path: %w", err)
		}
		if destFolder == "" {
			destFolder = `\\MOPSRV01\AdvoPro\Opgaver\Jurist\AutoImport\` + strconv.FormatUint(sagsnr, 10)
		}
	}
	db1.Close() // Done with pre-checks, release connection
	// --- End Phase 1 ---

	dbPath := strings.TrimRight(destFolder, "\\")

	// --- File operations (no DB connection held) ---
	localFolder := winPathToLocal(dbPath)
	if _, err := os.Stat(localFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(localFolder, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create dest folder %s: %w", localFolder, err)
		}
	}

	localDest := filepath.Join(localFolder, uniqueName)
	if err := copyFile(srcFilePath, localDest); err != nil {
		return nil, fmt.Errorf("failed to copy file: %w", err)
	}

	cleanupFile := func() {
		if _, statErr := os.Stat(localDest); statErr == nil {
			if rmErr := os.Remove(localDest); rmErr != nil {
				logger.Errorf("failed to remove temp file %s: %s", localDest, rmErr.Error())
			}
		}
	}

	// --- Phase 2: Fresh connection just for the transaction ---
	db, err := openAdvoPro()
	if err != nil {
		cleanupFile()
		return nil, err
	}
	defer db.Close()

	// --- Begin transaction ---
	tx, err := db.Begin()
	if err != nil {
		cleanupFile()
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// rollback on any error path
	rollback := func(cause error) (*ImportResult, error) {
		if rbErr := tx.Rollback(); rbErr != nil {
			logger.Errorf("failed to rollback document import: %s", rbErr.Error())
		}
		cleanupFile()
		return nil, cause
	}

	// 1. DokumentTabel
	dokID, err := execInsertReturnID(tx, "DokumentId", `
        INSERT INTO DokumentTabel (
            Titel, Medarbejdernr, VisEgenskaber, Standarddokument,
            MappeId, SlettefristId, PersonfolsommeData, Timer, Arbejdsart,
            VisMeddelelseStandardDokumentValgt, Behandlingskode, BehandlingAntalDage,
            TitelSomHistoriktekst, Oprettet, OprettetAf
        ) VALUES (@p1, @p2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, GETDATE(), @p3);
        SELECT SCOPE_IDENTITY();`,
		title, empID, user)
	if err != nil {
		return rollback(err)
	}

	// 2. DokumentVersion
	versionID, err := execInsertReturnID(tx, "DokumentVersionId", `
        INSERT INTO DokumentVersion (
            DokumentId, Versionsnr, Tekst, Navn, APEntryId,
            Lagertype, Placering, MasterVersionId, MaxVersion,
            Vedheftede, Subtype, Forsendelsesmetode, OpdelStatus, RealOprettet,
            Oprettet, OprettetAf, ForsendelseUdfortAf
        ) VALUES (@p1, 1, 'Version nr. 1', @p2, NEWID(), 2, @p3, 0, 0, 0, 0, -1, 0, GETDATE(), GETDATE(), @p4, '');
        SELECT SCOPE_IDENTITY();`,
		dokID, uniqueName, dbPath, user)
	if err != nil {
		return rollback(err)
	}

	// 3. DokumentForsendelse
	forsendelseID, err := execInsertReturnID(tx, "ForsendelseId", `
        INSERT INTO DokumentForsendelse (Sagsnr, Medarbejdernr, Levering, Kategori, Oprettet, OprettetAf, Tidspunkt)
        VALUES (@p1, @p2, 0, 0, GETDATE(), @p3, GETDATE());
        SELECT SCOPE_IDENTITY();`,
		sagsnr, empID, user)
	if err != nil {
		return rollback(err)
	}

	// 4. DokumentForsendelseDokument
	if _, err := tx.Exec(`
        INSERT INTO DokumentForsendelseDokument (ForsendelseId, DokumentId, DokumentVersionId, SkjulEksternt)
        VALUES (@p1, @p2, @p3, 0)`,
		forsendelseID, dokID, versionID); err != nil {
		return rollback(fmt.Errorf("insert DokumentForsendelseDokument: %w", err))
	}

	// 5. DokumentForsendelsePerson
	if _, err := tx.Exec(`
        INSERT INTO DokumentForsendelsePerson (ForsendelseId, Sagsnr_o4004, PersonId)
        VALUES (@p1, @p2, @p3)`,
		forsendelseID, sagsnr, sagsnr); err != nil {
		return rollback(fmt.Errorf("insert DokumentForsendelsePerson: %w", err))
	}

	// 6. DokumentForsendelseModtager
	if _, err := tx.Exec(`
        INSERT INTO DokumentForsendelseModtager (ForsendelseId, Persontype, PersonId, AttentionId, Adressering, Sagsnr_o4004, GruppeId, DetailId)
        VALUES (@p1, 1, @p2, 0, 1, @p3, 0, 0)`,
		forsendelseID, sagsnr, sagsnr); err != nil {
		return rollback(fmt.Errorf("insert DokumentForsendelseModtager: %w", err))
	}

	result := &ImportResult{
		DokID:         dokID,
		VersionID:     versionID,
		ForsendelseID: forsendelseID,
	}

	if dryRun {
		if rbErr := tx.Rollback(); rbErr != nil {
			logger.Errorf("failed to rollback dry-run document import: %s", rbErr.Error())
		}
		cleanupFile()
		logger.Infof("[DRY RUN] DB inserts rolled back. IDs would be: %d, %d, %d",
			dokID, versionID, forsendelseID)
		return result, nil
	}

	if err := tx.Commit(); err != nil {
		cleanupFile()
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return result, nil
}

// copyFile copies src to dst, preserving content (and best-effort mod time).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		if rmErr := os.Remove(dst); rmErr != nil {
			logger.Errorf("failed to remove partial file %s: %s", dst, rmErr.Error())
		}
		return err
	}
	if err := out.Close(); err != nil {
		if rmErr := os.Remove(dst); rmErr != nil {
			logger.Errorf("failed to remove file after close error %s: %s", dst, rmErr.Error())
		}
		return err
	}

	// best-effort: preserve modification time (shutil.copy2 behavior)
	if fi, statErr := os.Stat(src); statErr == nil {
		if ctErr := os.Chtimes(dst, time.Now(), fi.ModTime()); ctErr != nil {
			logger.Warnf("failed to preserve mod time for %s: %s", dst, ctErr.Error())
		}
	}
	return nil
}

// documentExists returns true if a document with the given title already
// exists on the case (sagsnr). Used to make imports idempotent.
func documentExists(db *sql.DB, sagsnr uint64, title string) (bool, error) {
	query := `
        SELECT COUNT(*)
        FROM DokumentForsendelse f
        JOIN DokumentForsendelseDokument fd ON fd.ForsendelseId = f.ForsendelseId
        JOIN DokumentTabel d ON d.DokumentId = fd.DokumentId
        WHERE f.Sagsnr = @p1 AND d.Titel = @p2`

	var count int
	err := db.QueryRow(query, sagsnr, title).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check for existing document: %w", err)
	}
	return count > 0, nil
}
