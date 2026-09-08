// penneo integration

// following the flow from postman

// a post endpoint exists which tells backend to start process (init when konsulent presses a start key)
// get access token
// ... wait for email confirm by frontend
// when confirmed send Casefile (templated file written by DAI)
//		save  jobs.uuid: 7a8f9381-9599-4647-99dc-f4a0a0... string and jobs.payloadHash = 106bf6a214dac95000840a7eb792... string
//
//  poll the job status with following body, keep polling until json.jobStatus === 'completed'
//		{
//  		"uuid": "{{jobUuid}}",
//  		"payloadHash": "{{payloadHash}}"
//		}
// then get the data casefileId
// casefileId = json.result.data.caseFile.id
//
// and then check how far along the debitor is
// in signing the document by checking {{baseUrl}}/api/v1/casefiles/{{casefileId}}
// perhaps get a webhook going. max 5 tho
//     "status": 1, means not signed yet, dont know what others mean but 5 means signed.
// get the documents.documentID : string 5VO39-9IV5G-I1ER9-...

// when signed get {{baseUrl}}/api/v3/documents/{{documentId}}/content for the signed pdf,
// and send it to the advopro integration for document upload
package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/gin-gonic/gin"
)

// =====================
// TYPES
// =====================

type PenneoTokenReq struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
	Key          string `json:"key"`
	Nonce        string `json:"nonce"`
	CreatedAt    string `json:"created_at"`
	Digest       string `json:"digest"`
}

type PenneoTokenResp struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type WebhookSubscriptionReq struct {
	EventTypes []string `json:"eventTypes"`
	Endpoint   string   `json:"endpoint"`
}

type WebhookSubscriptionResp struct {
	CustomerID int      `json:"customerId"`
	ID         string   `json:"id"`
	IsActive   bool     `json:"isActive"`
	Secret     string   `json:"secret"`
	EventTypes []string `json:"eventTypes"`
	Endpoint   string   `json:"endpoint"`
	UserID     int      `json:"userId"`
}

type PenneoWebhookEvent struct {
	EventType  string `json:"eventType"`
	CaseFileID string `json:"caseFileId"`
	SignerID   string `json:"signerId"`
	Timestamp  string `json:"timestamp"`
}

type JobStatusReq struct {
	UUID        string `json:"uuid"`
	PayloadHash string `json:"payloadHash"`
}

type CaseFileResp struct {
	ID        int `json:"id"`
	Status    int `json:"status"`
	Documents []struct {
		ID         int    `json:"id"`
		DocumentID string `json:"documentId"`
	} `json:"documents"`
}

type JobStatusResp struct {
	JobStatus string `json:"jobStatus"`
	Result    struct {
		Data struct {
			CaseFile struct {
				ID int64 `json:"id"`
			} `json:"caseFile"`
		} `json:"data"`
	} `json:"result"`
}

type PenneoJobState struct {
	UUID        string
	PayloadHash string
	AccessToken string
}

// =====================
// CONFIG
// =====================

var (
	authUrl = "https://login.penneo.com/"
	baseUrl = "https://app.penneo.com/"
)

// =====================
// SSE HUB — frontend listeners
// =====================

type SSEClient struct {
	Channel chan string
}

type SSEHub struct {
	mu      sync.RWMutex
	clients map[string][]*SSEClient // multiple listeners per casefile OK
}

var hub = &SSEHub{clients: make(map[string][]*SSEClient)}

func (h *SSEHub) Register(caseFileID string) *SSEClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	client := &SSEClient{Channel: make(chan string, 8)}
	h.clients[caseFileID] = append(h.clients[caseFileID], client)
	return client
}

func (h *SSEHub) Unregister(caseFileID string, client *SSEClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.clients[caseFileID]
	for i, c := range list {
		if c == client {
			h.clients[caseFileID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(h.clients[caseFileID]) == 0 {
		delete(h.clients, caseFileID)
	}
	close(client.Channel)
}

func (h *SSEHub) Notify(caseFileID, message string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients[caseFileID] {
		select {
		case c.Channel <- message:
		default:
		}
	}
}

// =====================
// PENDING JOBS — webhook → goroutine bridge
// =====================

type pendingJob struct {
	signed chan struct{} // closed when signed event received
}

type pendingRegistry struct {
	mu   sync.Mutex
	jobs map[string]*pendingJob
}

var pending = &pendingRegistry{jobs: make(map[string]*pendingJob)}

func (p *pendingRegistry) Add(caseFileID string) *pendingJob {
	p.mu.Lock()
	defer p.mu.Unlock()
	j := &pendingJob{signed: make(chan struct{})}
	p.jobs[caseFileID] = j
	return j
}

func (p *pendingRegistry) SignalSigned(caseFileID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if j, ok := p.jobs[caseFileID]; ok {
		select {
		case <-j.signed: // already closed
		default:
			close(j.signed)
		}
	}
}

func (p *pendingRegistry) Remove(caseFileID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.jobs, caseFileID)
}

// =====================
// PENNEO API CALLS
// =====================

func getAccessToken() (*PenneoTokenResp, error) {
	clientId := os.Getenv("PENNEO_CLIENTID")
	clientSecret := os.Getenv("PENNEO_CLIENTSECRET")
	apiKey := os.Getenv("PENNEO_APIKEY")
	apiSecret := os.Getenv("PENNEO_APISECRET")

	nonceBytes := make([]byte, 20) // 20 bytes like CryptoJS.random(20)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("gen nonce: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z") // ms precision, Z suffix

	h := sha1.New()
	h.Write(nonceBytes)
	h.Write([]byte(createdAt))
	h.Write([]byte(apiSecret))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	payload := PenneoTokenReq{
		ClientID: clientId, ClientSecret: clientSecret,
		GrantType: "api_keys", Key: apiKey,
		Nonce: nonce, CreatedAt: createdAt, Digest: digest,
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(authUrl+"oauth/token", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("post token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token status %d: %s", resp.StatusCode, string(b))
	}

	var result PenneoTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	return &result, nil
}

/*
	func createWebhookSubscription(accessToken string) (*WebhookSubscriptionResp, error) {
		endpoint := os.Getenv("PENNEO_WEBHOOK_ENDPOINT")
		reqBody := WebhookSubscriptionReq{
			EventTypes: []string{"sign.casefile.completed", "sign.signer.signed"},
			Endpoint:   endpoint,
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", baseUrl+"webhook/api/v1/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Auth-Token", accessToken)

		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("webhook sub: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusConflict {
			return nil, nil // already exists
		}
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("webhook sub status %d: %s", resp.StatusCode, string(b))
		}

		var result WebhookSubscriptionResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode webhook: %w", err)
		}
		os.Setenv("PENNEO_WEBHOOK_SECRET", result.Secret) // TODO: persist in DB
		return &result, nil
	}
*/
func sendCaseFile(accessToken string) (*PenneoJobState, error) {
	documentTitle := "Contract Document"
	documentName := "contract.pdf"
	documentDir := "./static/penneo_docs/"

	fullPath := filepath.Join(documentDir, documentName)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open doc: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	casefileData := map[string]interface{}{
		"caseFile": map[string]interface{}{
			"title": "Contract Agreement",
			"signers": []map[string]interface{}{
				{"name": "Markus kjeldsen", "email": "mkk@mop.dk", "role": "signer"},
			},
			"documents": []map[string]interface{}{
				{"title": documentTitle, "name": documentName},
			},
		},
	}
	caseFileJSON, _ := json.Marshal(casefileData)

	dataField, err := writer.CreateFormField("data")
	if err != nil {
		return nil, fmt.Errorf("form field: %w", err)
	}
	if _, err := dataField.Write(caseFileJSON); err != nil {
		return nil, fmt.Errorf("write data: %w", err)
	}

	filePart, err := writer.CreateFormFile("files", documentName)
	if err != nil {
		return nil, fmt.Errorf("form file: %w", err)
	}
	if _, err := io.Copy(filePart, file); err != nil {
		return nil, fmt.Errorf("copy file: %w", err)
	}
	writer.Close()

	//templateID := os.Getenv("PENNEO_TEMPLATE_ID")
	//if templateID == "" {
	//	return nil, fmt.Errorf("PENNEO_TEMPLATE_ID not set")
	//}
	req, _ := http.NewRequest("POST", baseUrl+"send/api/v1/casefiles/20251022/create", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Auth-Token", accessToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("send casefile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("casefile status %d: %s", resp.StatusCode, string(b))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode casefile: %w", err)
	}

	// jobs is array not map
	jobs, ok := result["jobs"].([]interface{})
	if !ok || len(jobs) == 0 {
		return nil, fmt.Errorf("missing jobs")
	}
	job, ok := jobs[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid job format")
	}
	uuid, _ := job["uuid"].(string)
	payloadHash, _ := job["payloadHash"].(string)
	if uuid == "" || payloadHash == "" {
		return nil, fmt.Errorf("missing uuid/payloadHash")
	}
	return &PenneoJobState{UUID: uuid, PayloadHash: payloadHash, AccessToken: accessToken}, nil
}

func pollJobStatus(state *PenneoJobState) (string, error) {
	body, _ := json.Marshal(JobStatusReq{UUID: state.UUID, PayloadHash: state.PayloadHash})
	client := &http.Client{}

	for i := 0; i < 12; i++ {
		req, _ := http.NewRequest("POST", baseUrl+"send/api/v1/queue/public/status", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Token", state.AccessToken)

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("job status: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return "", fmt.Errorf("job status %d: %s", resp.StatusCode, string(b))
		}
		var result JobStatusResp
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("decode job: %w", err)
		}

		if result.JobStatus == "completed" {
			id := result.Result.Data.CaseFile.ID
			if id == 0 {
				return "", fmt.Errorf("empty caseFileId")
			}
			return fmt.Sprintf("%d", id), nil
		}
		time.Sleep(500 * time.Millisecond)
		// they generate these things almost instantly so if the first one fails,
		// then by the time it takes to decode, it could be done
	}
	return "", fmt.Errorf("job poll timeout")
}

// fetchCaseFileStatus — used for fallback polling + verifying webhook
func fetchCaseFileStatus(accessToken, caseFileID string, retry bool) (*CaseFileResp, string, error) {
	req, _ := http.NewRequest("GET", baseUrl+"api/v1/casefiles/"+caseFileID, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, accessToken, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && retry {
		token, err := getAccessToken()
		if err != nil {
			return nil, accessToken, err
		}
		// Try once more with new token
		return fetchCaseFileStatus(token.AccessToken, caseFileID, false)
	}

	// 3. Handle other errors
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, accessToken, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var result CaseFileResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, accessToken, err
	}
	return &result, accessToken, nil
}

func getSignedDocument(accessToken string, documentID int) ([]byte, error) {
	u := fmt.Sprintf("https://app.penneo.com/api/v3/documents/%d/content", documentID)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/pdf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("penneo documents/%d/content: status %d: %s", documentID, resp.StatusCode, body)
	}

	return io.ReadAll(resp.Body)
}

// =====================
// BACKGROUND WORKER
// Waits for signed event (webhook or poll fallback), fetches PDF, uploads.
// =====================

func waitForSigningAndProcess(accessToken, caseFileID string) {
	defer pending.Remove(caseFileID)

	job := pending.Add(caseFileID)
	hub.Notify(caseFileID, `{"status":"awaiting_signature"}`)

	// Wait: webhook signals OR fallback poll every min OR hard timeout
	timeout := time.After(2 * time.Hour) // if they havent
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	signed := false
	status := 0
WaitLoop:
	for {
		select {
		case <-job.signed:
			signed = true
			break WaitLoop
		case <-ticker.C:
			// Fallback poll — covers missed webhooks
			cf, newTok, err := fetchCaseFileStatus(accessToken, caseFileID, true)
			accessToken = newTok
			if err != nil {
				logger.Errorf("[penneo] poll err %s: %v", caseFileID, err)
				continue
			}
			status = cf.Status
			switch cf.Status {
			case 5: // Completed
				signed = true
				break WaitLoop
			case 2: // Rejected
				hub.Notify(caseFileID, `{"status":"rejected"}`)
				break WaitLoop
			case 3: // Deleted
				hub.Notify(caseFileID, `{"status":"deleted"}`)
				return
			case 7: // Expired
				hub.Notify(caseFileID, `{"status":"expired"}`)
				return
			}

		case <-timeout:
			logger.Infof("[penneo] timeout waiting for %s", caseFileID)
			hub.Notify(caseFileID, `{"status":"timeout"}`)
			return
		}
	}

	if !signed {
		// TODO: implement what should happen if the person dosnt want to sign the doc
		switch status {
		case 0:
			logger.Info("Something went wrong, the status came back 0 draft")
		case 1:
			logger.Info("Something went wrong, the status came back 1 Pending")
		case 2:
			logger.Info("The status came back 2 rejected, i guess the debitor didnt like the visit")
		case 3:
			logger.Info("The file was deleted, status 3")
		case 5:
			logger.Info("This should not happen, that it is completed and not signed, status 5")
		case 7:
			logger.Info("if this happens then penneo has changed their experiation time, status 7")
		}
		return
	}

	hub.Notify(caseFileID, `{"status":"signed"}`)

	cf, newTok, err := fetchCaseFileStatus(accessToken, caseFileID, true)
	accessToken = newTok
	if err != nil || len(cf.Documents) == 0 {
		logger.Errorf("[penneo] fetch docs failed %s: %v", caseFileID, err)
		hub.Notify(caseFileID, `{"status":"error","message":"fetch documents failed"}`)
		return
	}

	pdfBytes, err := getSignedDocument(accessToken, cf.Documents[0].ID)
	if err != nil {
		logger.Errorf("[penneo] get pdf failed %s: %v", caseFileID, err)
		hub.Notify(caseFileID, `{"status":"error","message":"get pdf failed"}`)
		return
	}

	// TODO: Advoproupload(pdfbytes)

	logger.Infof("[penneo] got signed pdf %s, %d bytes", caseFileID, len(pdfBytes))
	hub.Notify(caseFileID, fmt.Sprintf(`{"status":"completed","documentId":%d,"size":%d}`, cf.Documents[0].ID, len(pdfBytes)))
}

// =====================
// HANDLERS
// =====================

// POST /penneo/start — kicks off flow, returns caseFileId fast
func StartPenneoFlow(c *gin.Context) {
	tokenResp, err := getAccessToken()
	if err != nil {
		logger.Errorf("[Penneo] AccessToken error %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token", "details": err.Error()})
		return
	}

	jobState, err := sendCaseFile(tokenResp.AccessToken)
	if err != nil {
		logger.Errorf("[Penneo] sendCaseFile error %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "send casefile", "details": err.Error()})
		return
	}

	caseFileID, err := pollJobStatus(jobState)
	if err != nil {
		logger.Errorf("[Penneo] pollJobStatus error %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "poll job", "details": err.Error()})
		return
	}

	// Spawn background waiter
	go waitForSigningAndProcess(tokenResp.AccessToken, caseFileID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Penneo flow started, awaiting signature",
		"caseFileId": caseFileID,
		"sseUrl":     "/penneo/events/" + caseFileID,
	})
}

// POST /penneo/webhook — Penneo calls this on sign events
func PenneoWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Errorf("[Penneo] PenneoWebhook body read error %s", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}

	// HMAC verify
	secret := os.Getenv("PENNEO_WEBHOOK_SECRET")
	sig := c.GetHeader("X-Penneo-Signature")
	if secret != "" && sig != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad signature"})
			return
		}
	}

	var ev PenneoWebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		logger.Errorf("[Penneo] bad json error %s", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}

	logger.Infof("[penneo webhook] %s for %s", ev.EventType, ev.CaseFileID)

	if ev.EventType == "sign.casefile.completed" {
		pending.SignalSigned(ev.CaseFileID)
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}

// GET /penneo/events/:caseFileId — SSE stream for frontend
func PenneoSSE(c *gin.Context) {
	caseFileID := c.Param("caseFileId")
	if caseFileID == "" {
		logger.Info("[Penneo] missing caseFileId")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing caseFileId"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	client := hub.Register(caseFileID)
	defer hub.Unregister(caseFileID, client)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logger.Error("[Penneo] streaming unsupported error")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	// Initial ping
	fmt.Fprintf(c.Writer, "event: ping\ndata: connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case msg, ok := <-client.Channel:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", msg)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(c.Writer, ": keepalive\n\n")
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
