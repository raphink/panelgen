// Package api provides a minimal client for the OpenAI and Azure OpenAI image generation APIs.
package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provider selects between the standard OpenAI API and Azure OpenAI.
type Provider int

const (
	ProviderOpenAI Provider = iota
	ProviderAzure
)

// Client talks to the OpenAI or Azure OpenAI images API.
type Client struct {
	Provider   Provider
	Endpoint   string
	APIKey     string
	Deployment string // model name (OpenAI) or deployment name (Azure)
	APIVersion string // Azure only
	HTTPClient *http.Client
}

// NewClientFromEnv creates a Client from environment variables.
//
// Azure is used when AZURE_OPENAI_ENDPOINT is set:
//
//	AZURE_OPENAI_ENDPOINT   https://your-resource.openai.azure.com
//	AZURE_OPENAI_API_KEY    your-key
//	AZURE_OPENAI_DEPLOYMENT deployment name (default: gpt-image-2)
//
// Standard OpenAI is used when OPENAI_API_KEY is set:
//
//	OPENAI_API_KEY   your-key
//	OPENAI_BASE_URL  base URL (default: https://api.openai.com)
//	OPENAI_MODEL     model name (default: gpt-image-2)
func NewClientFromEnv() (*Client, error) {
	if azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT"); azureEndpoint != "" {
		apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_API_KEY must be set when AZURE_OPENAI_ENDPOINT is set")
		}
		deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
		if deployment == "" {
			deployment = "gpt-image-2"
		}
		return &Client{
			Provider:   ProviderAzure,
			Endpoint:   strings.TrimRight(azureEndpoint, "/"),
			APIKey:     apiKey,
			Deployment: deployment,
			APIVersion: "2025-04-01-preview",
			HTTPClient: &http.Client{Timeout: 300 * time.Second},
		}, nil
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		endpoint := os.Getenv("OPENAI_BASE_URL")
		if endpoint == "" {
			endpoint = "https://api.openai.com"
		}
		model := os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = "gpt-image-2"
		}
		return &Client{
			Provider:   ProviderOpenAI,
			Endpoint:   strings.TrimRight(endpoint, "/"),
			APIKey:     apiKey,
			Deployment: model,
			HTTPClient: &http.Client{Timeout: 300 * time.Second},
		}, nil
	}

	return nil, fmt.Errorf(
		"set AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_API_KEY (Azure) or OPENAI_API_KEY (OpenAI)",
	)
}

func (c *Client) generationsURL() string {
	if c.Provider == ProviderAzure {
		return fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s",
			c.Endpoint, c.Deployment, c.APIVersion)
	}
	return c.Endpoint + "/v1/images/generations"
}

func (c *Client) editsURL() string {
	if c.Provider == ProviderAzure {
		return fmt.Sprintf("%s/openai/deployments/%s/images/edits?api-version=%s",
			c.Endpoint, c.Deployment, c.APIVersion)
	}
	return c.Endpoint + "/v1/images/edits"
}

func (c *Client) setAuth(req *http.Request) {
	if c.Provider == ProviderAzure {
		req.Header.Set("api-key", c.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
}

// GenerateRequest is the body for the /images/generations endpoint.
type GenerateRequest struct {
	Model        string `json:"model,omitempty"` // OpenAI only
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
	if c.Provider == ProviderOpenAI {
		reqBody.Model = c.Deployment
	}
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
	c.setAuth(req)

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
	if c.Provider == ProviderOpenAI {
		if err := w.WriteField("model", c.Deployment); err != nil {
			return nil, "", err
		}
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
	c.setAuth(req)

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
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}
