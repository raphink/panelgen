package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateOpenAIRequest(t *testing.T) {
	imageData := []byte("png-data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %q, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}

		var body GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "gpt-image-2" {
			t.Errorf("model = %q", body.Model)
		}
		if body.Prompt != "draw a fox" {
			t.Errorf("prompt = %q", body.Prompt)
		}
		if body.Size != "1024x1024" {
			t.Errorf("size = %q", body.Size)
		}
		if body.Quality != "high" {
			t.Errorf("quality = %q", body.Quality)
		}
		if body.OutputFormat != "png" {
			t.Errorf("output_format = %q", body.OutputFormat)
		}
		writeImageResponse(t, w, imageData)
	}))
	defer server.Close()

	client := &Client{
		Provider:   ProviderOpenAI,
		Endpoint:   server.URL,
		APIKey:     "test-key",
		Deployment: "gpt-image-2",
		HTTPClient: server.Client(),
	}
	got, err := client.Generate("draw a fox", "1024x1024", "high")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(imageData) {
		t.Fatalf("image data = %q", got)
	}
}

func TestEditAzureMultipartRequest(t *testing.T) {
	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("ref-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	imageData := []byte("edited-png-data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/openai/deployments/image-deployment/images/edits"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %s", r.URL.Path, wantPath)
		}
		if got := r.URL.Query().Get("api-version"); got != "2025-04-01-preview" {
			t.Fatalf("api-version = %q", got)
		}
		if got := r.Header.Get("api-key"); got != "azure-key" {
			t.Fatalf("api-key = %q", got)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatal(err)
		}

		assertFormValue(t, r, "prompt", "use this reference")
		assertFormValue(t, r, "n", "1")
		assertFormValue(t, r, "size", "1536x1024")
		assertFormValue(t, r, "quality", "medium")
		assertFormValue(t, r, "output_format", "png")
		if _, ok := r.MultipartForm.Value["model"]; ok {
			t.Fatal("Azure edit request should not include model field")
		}

		files := r.MultipartForm.File["image[]"]
		if len(files) != 1 {
			t.Fatalf("image[] file count = %d", len(files))
		}
		if files[0].Filename != "ref.png" {
			t.Fatalf("filename = %q", files[0].Filename)
		}
		if got := files[0].Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("image content type = %q", got)
		}
		f, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		refBytes, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(refBytes) != "ref-bytes" {
			t.Fatalf("ref bytes = %q", refBytes)
		}

		writeImageResponse(t, w, imageData)
	}))
	defer server.Close()

	client := &Client{
		Provider:   ProviderAzure,
		Endpoint:   server.URL,
		APIKey:     "azure-key",
		Deployment: "image-deployment",
		APIVersion: "2025-04-01-preview",
		HTTPClient: server.Client(),
	}
	got, err := client.Edit("use this reference", []string{refPath}, "1536x1024", "medium")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(imageData) {
		t.Fatalf("image data = %q", got)
	}
}

func TestGenerateReturnsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request","code":"bad_request"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := &Client{
		Provider:   ProviderOpenAI,
		Endpoint:   server.URL,
		APIKey:     "test-key",
		Deployment: "gpt-image-2",
		HTTPClient: server.Client(),
	}
	_, err := client.Generate("draw a fox", "1024x1024", "high")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseImageResponseAPIError(t *testing.T) {
	_, err := parseImageResponse([]byte(`{"error":{"message":"blocked","code":"policy"}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "policy") || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertFormValue(t *testing.T, r *http.Request, key, want string) {
	t.Helper()
	got := r.MultipartForm.Value[key]
	if len(got) != 1 || got[0] != want {
		t.Fatalf("form %s = %v, want %q", key, got, want)
	}
}

func writeImageResponse(t *testing.T, w http.ResponseWriter, imageData []byte) {
	t.Helper()
	resp := map[string]any{
		"data": []map[string]string{
			{"b64_json": base64.StdEncoding.EncodeToString(imageData)},
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatal(err)
	}
}
