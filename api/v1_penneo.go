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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

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

// Penneo sends this to our webhook endpoint
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
	Status    int `json:"status"`
	Documents []struct {
		DocumentID string `json:"documentId"`
	} `json:"documents"`
}

type JobStatusResp struct {
	JobStatus string `json:"jobStatus"`
	Result    struct {
		Data struct {
			CaseFile struct {
				ID string `json:"id"`
			} `json:"caseFile"`
		} `json:"data"`
	} `json:"result"`
}

// Stored between steps
type PenneoJobState struct {
	UUID        string
	PayloadHash string
	AccessToken string
}

// SSE client to push updates to frontend
type SSEClient struct {
	CaseFileID string
	Channel    chan string
}

// =====================
// SSE HUB
// Keeps track of frontend listeners waiting for updates
// =====================

type SSEHub struct {
	mu      sync.RWMutex
	clients map[string]*SSEClient // keyed by caseFileId
}

var hub = &SSEHub{
	clients: make(map[string]*SSEClient),
}

func (h *SSEHub) Register(caseFileID string) *SSEClient {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := &SSEClient{
		CaseFileID: caseFileID,
		Channel:    make(chan string, 1),
	}
	h.clients[caseFileID] = client
	return client
}

func (h *SSEHub) Notify(caseFileID string, message string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if client, ok := h.clients[caseFileID]; ok {
		select {
		case client.Channel <- message:
		default:
			// Client channel full, skip
		}
	}
}

func (h *SSEHub) Unregister(caseFileID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, caseFileID)
}

var authUrl = "https://login.penneo.com"
var baseUrl = "https://app.penneo.com"

func getAccessToken(c *gin.Context) (*PenneoTokenResp, error) {
	clientId := os.Getenv("PENNEO_CLIENTID")
	clientSecret := os.Getenv("PENNEO_CLIENTSECRET")
	ApiKey := os.Getenv("PENNEO_APIKEY")
	ApiSecret := os.Getenv("PENNEO_APISECRET")

	// Generate nonce
	nonceBytes := make([]byte, 16)
	rand.Read(nonceBytes)
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)

	// ISO 8601 timestamp
	createdAt := time.Now().UTC().Format(time.RFC3339)

	// SHA256 digest
	h := sha256.New()
	h.Write([]byte(nonce + createdAt + ApiSecret))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	payload := PenneoTokenReq{
		ClientID:     clientId,
		ClientSecret: clientSecret,
		GrantType:    "api_keys",
		Key:          ApiKey,
		Nonce:        nonce,
		CreatedAt:    createdAt,
		Digest:       digest,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token request: %w", err)
	}

	resp, err := http.Post(authUrl+"oauth/token", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to post token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result PenneoTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &result, nil
}

func createWebhookSubscription(accessToken string) (*WebhookSubscriptionResp, error) {
	// This is your backend's public URL that Penneo will call
	webhookEndpoint := os.Getenv("PENNEO_WEBHOOK_ENDPOINT") // e.g. "https://yourdomain.com/api/penneo/webhook"

	reqBody := WebhookSubscriptionReq{
		EventTypes: []string{
			"sign.casefile.completed",
			"sign.signer.signed",
		},
		Endpoint: webhookEndpoint,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal webhook subscription request: %w", err)
	}

	req, err := http.NewRequest("POST", baseUrl+"webhook/api/v1/subscriptions", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook subscription request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Token", accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook subscription: %w", err)
	}
	defer resp.Body.Close()

	// 409 means subscription already exists for this endpoint, which is fine
	// we just return nil and continue — Penneo will still fire events to our endpoint
	if resp.StatusCode == http.StatusConflict {
		fmt.Println("Webhook subscription already exists, reusing existing subscription")
		return nil, nil
	}

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("webhook subscription failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result WebhookSubscriptionResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode webhook subscription response: %w", err)
	}
	// TODO: Store in DB
	// Store the secret for later HMAC verification of incoming webhook calls
	// In production store this in your DB tied to the subscription ID
	os.Setenv("PENNEO_WEBHOOK_SECRET", result.Secret)

	return &result, nil
}

func sendCaseFile(accessToken string) (*PenneoJobState, error) {

	documentTitle := "Contract Document"
	documentName := "contract.pdf"
	documentPath := "./static/penneo_docs/"

	filepath := filepath.Join(documentPath, documentName)
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open document: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	casefileData := map[string]interface{}{
		"caseFile": map[string]interface{}{
			"title": "Contract Agreement",
			"signers": []map[string]interface{}{
				{
					"name":  "Markus kjeldsen",
					"email": "mkk@mop.dk",
					"role":  "signer",
				},
			},
			"documents": []map[string]interface{}{
				{
					"title": documentTitle,
					"name":  documentName,
				},
			},
		},
	}

	caseFileJSON, err := json.Marshal(casefileData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal casefile data %w", err)
	}

	dataField, err := writer.CreateFormField("data")
	if err != nil {
		return nil, fmt.Errorf("failed to create formfield: %w", err)
	}
	dataField.Write(caseFileJSON)

	filePart, err := writer.CreateFormFile("files", documentName)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(filePart, file); err != nil {
		return nil, fmt.Errorf("failed to copy file contents: %w", err)
	}

	writer.Close()

	req, err := http.NewRequest("POST", baseUrl+"send/api/v1/casefiles/20251022/create", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create casefile request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Auth-Token", accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send casefile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("casefile request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode casefile response: %w", err)
	}

	jobs, ok := result["jobs"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format, missing jobs field")
	}

	uuid, _ := jobs["uuid"].(string)
	payloadHash, _ := jobs["payloadHash"].(string)

	if uuid == "" || payloadHash == "" {
		return nil, fmt.Errorf("missing uuid: %s or payloadHash: %s", uuid, payloadHash)
	}

	return &PenneoJobState{
		UUID:        uuid,
		PayloadHash: payloadHash,
		AccessToken: accessToken,
	}, nil
}

func pollJobStatus(state *PenneoJobState) (string, error) {
	reqBody := JobStatusReq{
		UUID:        state.UUID,
		PayloadHash: state.PayloadHash,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal job status request: %w", err)
	}

	client := &http.Client{}

	// Try up to 12 times with 5 second gaps = max 60 seconds of waiting.
	// If the job isnt done by then, something is probably wrong.
	for i := 0; i < 12; i++ {
		req, err := http.NewRequest("GET", baseUrl+"api/v1/jobs/status", bytes.NewBuffer(body))
		if err != nil {
			return "", fmt.Errorf("failed to create job status request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Token", state.AccessToken)

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to get job status: %w", err)
		}

		var result JobStatusResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode job status response: %w", err)
		}
		resp.Body.Close()

		if result.JobStatus == "completed" {
			caseFileID := result.Result.Data.CaseFile.ID
			if caseFileID == "" {
				return "", fmt.Errorf("job completed but caseFileId is empty")
			}
			return caseFileID, nil
		}

		// Wait 5 seconds before trying again.
		// time.Sleep blocks the current goroutine (not the whole server).
		time.Sleep(5 * time.Second)
	}

	return "", fmt.Errorf("job polling timed out after 1 minute")
}

func getCaseFileDocuments(accessToken, caseFileID string) (string, error) {
	req, err := http.NewRequest("GET", baseUrl+"api/v1/casefiles/"+caseFileID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create casefile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get casefile: %w", err)
	}
	defer resp.Body.Close()

	var result CaseFileResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode casefile response: %w", err)
	}

	if len(result.Documents) == 0 {
		return "", fmt.Errorf("no documents found in casefile")
	}

	// Return the first document's ID.
	// TODO: FIX MULTIPLE DOCUMENT IDs
	// If there are multiple documents, you may need to handle that differently.
	return result.Documents[0].DocumentID, nil
}

func getSignedDocument(accessToken, documentID string) ([]byte, error) {
	req, err := http.NewRequest("GET", baseUrl+"api/v3/documents/"+documentID+"/content", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create document content request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get signed document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("document content request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	pdfBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read document content: %w", err)
	}

	return pdfBytes, nil
}

// =====================
// GIN HANDLER: START PENNEO FLOW
// =====================

func StartPenneoFlow(c *gin.Context) {
	// Step 1: Get access token
	tokenResp, err := getAccessToken(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get access token", "details": err.Error()})
		return
	}

	// Step 2: Send casefile
	jobState, err := sendCaseFile(tokenResp.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send casefile", "details": err.Error()})
		return
	}

	// Step 3: Poll job until completed, get caseFileId
	caseFileID, err := pollJobStatus(jobState)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to poll job status", "details": err.Error()})
		return
	}

	// Step 4: Poll casefile until signed, get documentId
	// the wrong func
	documentID, err := pollJobStatus(jobState) // , caseFileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to poll casefile signing", "details": err.Error()})
		return
	}

	// Step 5: Get signed PDF bytes
	pdfBytes, err := getSignedDocument(tokenResp.AccessToken, documentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get signed document", "details": err.Error()})
		return
	}

	// TODO: Send pdfBytes to AdvoPro integration
	// uploadToAdvoPro(pdfBytes)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Penneo flow completed successfully",
		"caseFileId": caseFileID,
		"documentId": documentID,
		"pdfSize":    len(pdfBytes),
	})
}
