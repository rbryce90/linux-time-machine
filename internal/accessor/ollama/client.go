// Package ollama is a minimal HTTP client for a local Ollama server.
// Shared across domains that need embeddings or chat completions.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultBaseURL = "http://localhost:11434"

type Client struct {
	baseURL        string
	http           *http.Client
	embeddingModel string
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = u } }
func WithEmbeddingModel(m string) Option   { return func(c *Client) { c.embeddingModel = m } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func New(opts ...Option) *Client {
	c := &Client{
		baseURL:        DefaultBaseURL,
		http:           &http.Client{Timeout: 30 * time.Second},
		embeddingModel: "nomic-embed-text",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) EmbeddingModel() string { return c.embeddingModel }

// Ping returns nil if the server responds to /api/version.
func (c *Client) Ping(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping: %s", resp.Status)
	}
	return nil
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed returns the embedding vector for the given text, using the
// configured embedding model.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.embeddingModel, Prompt: text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: status %s: %s", resp.Status, string(b))
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("ollama embed: decode: %w", err)
	}
	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding")
	}
	return er.Embedding, nil
}
