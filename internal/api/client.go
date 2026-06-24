// Package api provides a minimal OpenAI-compatible image generation client.
package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible images API.
type Client struct {
	Endpoint   string
	APIKey     string
	Deployment string
	HTTPClient *http.Client
}

// NewClientFromEnv creates a Client from environment variables.
//
//	OPENAI_API_KEY   your-key
//	OPENAI_BASE_URL  base URL (default: https://api.openai.com/v1)
//	OPENAI_MODEL     model name or deployment name (default: gpt-image-2)
func NewClientFromEnv() (*Client, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY must be set")
	}
	endpoint := os.Getenv("OPENAI_BASE_URL")
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-image-2"
	}
	return &Client{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		APIKey:     apiKey,
		Deployment: model,
		HTTPClient: &http.Client{Timeout: 300 * time.Second},
	}, nil
}

func (c *Client) generationsURL() string {
	return c.Endpoint + "/images/generations"
}

func (c *Client) editsURL() string {
	return c.Endpoint + "/images/edits"
}

// GenerateRequest is the body for the /images/generations endpoint.
type GenerateRequest struct {
	Model        string `json:"model,omitempty"`
	Prompt       string `json:"prompt"`
	N            int    `json:"n"`
	Size         string `json:"size"`
	Quality      string `json:"quality"`
	OutputFormat string `json:"output_format"`
}

// ImageResponse is the shared response shape.
type ImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Error *APIError `json:"error,omitempty"`
}

type APIError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Generate calls the images/generations endpoint (no reference images).
func (c *Client) Generate(prompt, size, quality string) ([]byte, error) {
	url := c.generationsURL()

	reqBody := GenerateRequest{
		Prompt:       prompt,
		N:            1,
		Size:         size,
		Quality:      quality,
		OutputFormat: "png",
	}
	reqBody.Model = c.Deployment
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	return c.doJSON(url, body)
}

// Edit calls the images/edits endpoint with one or more reference images.
func (c *Client) Edit(prompt string, refs []string, size, quality string) ([]byte, error) {
	url := c.editsURL()

	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		data, err := c.doEdit(url, prompt, refs, size, quality)
		if err != nil {
			if strings.Contains(err.Error(), "500") && attempt < maxAttempts {
				wait := time.Duration(attempt*10) * time.Second
				fmt.Fprintf(os.Stderr, "  Server error (attempt %d/%d), retrying in %s...\n",
					attempt, maxAttempts, wait)
				time.Sleep(wait)
				continue
			}
			return nil, err
		}
		return data, nil
	}
	return nil, fmt.Errorf("edit failed after %d attempts", maxAttempts)
}

func (c *Client) doEdit(url, prompt string, refs []string, size, quality string) ([]byte, error) {
	buf, contentType, err := c.buildEditForm(prompt, refs, size, quality)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, string(respBody))
	}
	return parseImageResponse(respBody)
}

func (c *Client) buildEditForm(prompt string, refs []string, size, quality string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	for _, field := range [][2]string{
		{"prompt", prompt}, {"n", "1"}, {"size", size}, {"quality", quality}, {"output_format", "png"},
	} {
		if err := w.WriteField(field[0], field[1]); err != nil {
			return nil, "", err
		}
	}
	if err := w.WriteField("model", c.Deployment); err != nil {
		return nil, "", err
	}

	for _, ref := range refs {
		f, err := os.Open(ref)
		if err != nil {
			return nil, "", fmt.Errorf("open reference %s: %w", ref, err)
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="%s"`, filepath.Base(ref)))
		h.Set("Content-Type", mimeFromExt(filepath.Ext(ref)))
		part, err := w.CreatePart(h)
		if err != nil {
			f.Close()
			return nil, "", err
		}
		_, copyErr := io.Copy(part, f)
		f.Close()
		if copyErr != nil {
			return nil, "", copyErr
		}
	}
	w.Close()
	return &buf, w.FormDataContentType(), nil
}

func (c *Client) doJSON(url string, body []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, string(respBody))
	}
	return parseImageResponse(respBody)
}

func parseImageResponse(body []byte) ([]byte, error) {
	var imgResp ImageResponse
	if err := json.Unmarshal(body, &imgResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if imgResp.Error != nil {
		return nil, fmt.Errorf("API error %s: %s", imgResp.Error.Code, imgResp.Error.Message)
	}
	if len(imgResp.Data) == 0 {
		return nil, fmt.Errorf("no images in response")
	}
	return base64.StdEncoding.DecodeString(imgResp.Data[0].B64JSON)
}

func mimeFromExt(ext string) string {
	if t := mime.TypeByExtension(strings.ToLower(ext)); t != "" {
		// Strip parameters (e.g. "image/jpeg; charset=...") — API expects bare type.
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		return t
	}
	return "image/png"
}
