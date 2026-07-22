package upload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tandemhealth/gh-image/internal/cookies"
	"github.com/tandemhealth/gh-image/internal/httputil"
)

// Result holds the output of a successful file upload.
type Result struct {
	URL      string // https://github.com/user-attachments/assets/<uuid> (images) or /files/<id>/<name>
	Name     string // sanitized filename
	Markdown string // ![name](url)
}

// policyResponse represents the JSON response from /upload/policies/assets.
type policyResponse struct {
	UploadURL string `json:"upload_url"`
	Asset     struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Size        int64  `json:"size"`
		ContentType string `json:"content_type"`
		Href        string `json:"href"`
	} `json:"asset"`
	Form map[string]string `json:"form"`
	// AssetUploadURL is the server-provided path used to finalize the PNG.
	AssetUploadURL               string `json:"asset_upload_url"`
	AssetUploadAuthenticityToken string `json:"asset_upload_authenticity_token"`
}

// Client carries the HTTP client (with GitHub session cookies) and the base URL
// for GitHub requests. baseURL has no trailing slash; the production value is
// "https://github.com". Tests point it at an httptest server.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient creates a Client with the GitHub session cookies set.
// GitHub requires both user_session and __Host-user_session_same_site
// for CSRF validation on the upload endpoint.
func NewClient(sessionCookie *http.Cookie) *Client {
	return &Client{
		http: &http.Client{
			Jar:     cookies.NewGitHubCookieJar(sessionCookie),
			Timeout: 30 * time.Second,
		},
		baseURL: "https://github.com",
	}
}

// Upload uploads a file to GitHub and returns the asset URL.
// owner/repo identifies the target repository, repoID is its numeric ID.
func (c *Client) Upload(owner, repo string, repoID int, evidence *Evidence) (*Result, error) {
	if evidence == nil {
		return nil, fmt.Errorf("evidence is required")
	}
	contentType := "image/png"
	fileName := evidence.Name()

	// Step 0: Get uploadToken from repo page
	uploadToken, err := c.getUploadToken(owner, repo)
	if err != nil {
		return nil, fmt.Errorf("step 0 (get upload token): %w", err)
	}

	// Step 1: Request upload policy
	policy, err := c.requestPolicy(owner, repo, uploadToken, repoID, fileName, evidence.Size(), contentType)
	if err != nil {
		return nil, fmt.Errorf("step 1 (request policy): %w", err)
	}

	// Step 2: Upload file to S3
	err = uploadToS3(policy, evidence.Reader(), fileName, contentType)
	if err != nil {
		return nil, fmt.Errorf("step 2 (S3 upload): %w", err)
	}

	// Step 3: Finalize the upload
	result, err := c.finalizeUpload(owner, repo, policy)
	if err != nil {
		return nil, fmt.Errorf("step 3 (finalize): %w", err)
	}

	return result, nil
}

// requestPolicy calls POST /upload/policies/assets to get the S3 presigned form.
func (c *Client) requestPolicy(owner, repo, uploadToken string, repoID int, fileName string, fileSize int64, contentType string) (*policyResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for _, f := range []struct{ k, v string }{
		{"name", fileName},
		{"size", strconv.FormatInt(fileSize, 10)},
		{"content_type", contentType},
		{"authenticity_token", uploadToken},
		{"repository_id", strconv.Itoa(repoID)},
	} {
		if err := writer.WriteField(f.k, f.v); err != nil {
			return nil, fmt.Errorf("writing form field %s: %w", f.k, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/upload/policies/assets", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", fmt.Sprintf("%s/%s/%s", c.baseURL, owner, repo))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", httputil.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("expected 201, got %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var policy policyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
		return nil, fmt.Errorf("decoding policy response: %w", err)
	}

	if policy.UploadURL == "" {
		return nil, fmt.Errorf("policy response missing upload_url")
	}
	if policy.AssetUploadAuthenticityToken == "" {
		return nil, fmt.Errorf("policy response missing asset_upload_authenticity_token")
	}
	if len(policy.Form) == 0 {
		return nil, fmt.Errorf("policy response missing form fields")
	}
	if policy.Asset.ID == 0 {
		return nil, fmt.Errorf("policy response missing asset ID")
	}
	if policy.AssetUploadURL == "" {
		return nil, fmt.Errorf("policy response missing asset_upload_url")
	}
	if !strings.HasPrefix(policy.AssetUploadURL, "/") {
		return nil, fmt.Errorf("policy response asset_upload_url %q is not a root-relative path", policy.AssetUploadURL)
	}
	// content_type drives the render form (embed / video player / link), so a
	// response without it is unusable even though the upload itself would succeed.
	if policy.Asset.ContentType == "" {
		return nil, fmt.Errorf("policy response missing asset content_type")
	}
	if policy.Asset.ContentType != "image/png" {
		return nil, fmt.Errorf("policy response returned unexpected asset content_type %q", policy.Asset.ContentType)
	}

	return &policy, nil
}

// finalizeUpload PUTs to the policy's asset_upload_url to mark the asset as
// ready, then builds the markdown reference. The render form keys off
// policy.Asset.ContentType — GitHub's echo of the content_type we sent in the
// policy request, not an independent classification. In practice it tracks how
// GitHub routes and renders the asset (verified incl. SVG, which routes to the
// image path and embeds inline); the href shape (/assets/ vs /files/) is the
// only strictly authoritative routing signal if these ever diverge.
func (c *Client) finalizeUpload(owner, repo string, policy *policyResponse) (*Result, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("authenticity_token", policy.AssetUploadAuthenticityToken); err != nil {
		return nil, fmt.Errorf("writing form field authenticity_token: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest("PUT", c.baseURL+policy.AssetUploadURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", fmt.Sprintf("%s/%s/%s", c.baseURL, owner, repo))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", httputil.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("expected 200, got %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding finalize response: %w", err)
	}

	return &Result{
		URL:      result.Href,
		Name:     result.Name,
		Markdown: fmt.Sprintf("![%s](%s)", result.Name, result.Href),
	}, nil
}

func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}
