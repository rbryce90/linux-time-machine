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
	chatModel      string
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = u } }
func WithEmbeddingModel(m string) Option   { return func(c *Client) { c.embeddingModel = m } }
func WithChatModel(m string) Option        { return func(c *Client) { c.chatModel = m } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func New(opts ...Option) *Client {
	// Localhost, possibly many concurrent callers (embedder + chat + ping).
	// Default MaxIdleConnsPerHost of 2 thrashes under load; disable gzip
	// since it's localhost. Request deadlines come from callers' ctx.
	transport := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}
	c := &Client{
		baseURL:        DefaultBaseURL,
		http:           &http.Client{Transport: transport},
		embeddingModel: "nomic-embed-text",
		chatModel:      "llama3.1:8b",
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

// postJSON is the shared request pipeline for the JSON endpoints on /api/*.
// The body-read-on-error branch lets callers see the server's message
// instead of just a status code.
func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %s: %s", resp.Status, string(b))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode: %w (body: %s)", err, truncate(string(b), 500))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	var er embedResponse
	if err := c.postJSON(ctx, "/api/embeddings",
		embedRequest{Model: c.embeddingModel, Prompt: text}, &er); err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding")
	}
	return er.Embedding, nil
}
