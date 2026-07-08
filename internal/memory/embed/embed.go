// Package embed abstracts embedding providers for the memory store. Every
// provider degrades to nil (FTS-only search) when unconfigured — memory must
// never hard-fail because a key is missing.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
)

const (
	InputDocument = "document"
	InputQuery    = "query"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string, inputType string) ([][]float32, error)
	// Model identifies provider+model+dims, stamped into rows so recall only
	// trusts vectors produced by the current configuration.
	Model() string
	Dims() int
}

// New builds the configured embedder, or nil when provider is none/missing key.
func New(cfg config.EmbeddingsConfig) Embedder {
	switch cfg.Provider {
	case "", "none":
		return nil
	case "voyage":
		key := apiKey(cfg, "VOYAGE_API_KEY")
		if key == "" {
			return nil
		}
		return &httpEmbedder{
			provider: "voyage",
			model:    defaultStr(cfg.Model, "voyage-3.5-lite"),
			dims:     defaultInt(cfg.Dimensions, 1024),
			key:      key,
			url:      defaultStr(cfg.BaseURL, "https://api.voyageai.com/v1") + "/embeddings",
		}
	case "openai":
		key := apiKey(cfg, "OPENAI_API_KEY")
		if key == "" {
			return nil
		}
		return &httpEmbedder{
			provider: "openai",
			model:    defaultStr(cfg.Model, "text-embedding-3-small"),
			dims:     defaultInt(cfg.Dimensions, 1024),
			key:      key,
			url:      defaultStr(cfg.BaseURL, "https://api.openai.com/v1") + "/embeddings",
		}
	case "ollama":
		return &httpEmbedder{
			provider: "ollama",
			model:    defaultStr(cfg.Model, "nomic-embed-text"),
			dims:     defaultInt(cfg.Dimensions, 768),
			url:      defaultStr(cfg.BaseURL, "http://localhost:11434") + "/api/embed",
		}
	default:
		return nil
	}
}

type httpEmbedder struct {
	provider string
	model    string
	dims     int
	key      string
	url      string
}

func (e *httpEmbedder) Model() string { return fmt.Sprintf("%s/%s@%d", e.provider, e.model, e.dims) }
func (e *httpEmbedder) Dims() int     { return e.dims }

func (e *httpEmbedder) Embed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var body map[string]any
	switch e.provider {
	case "voyage":
		body = map[string]any{"input": texts, "model": e.model, "input_type": inputType, "output_dimension": e.dims}
	case "openai":
		body = map[string]any{"input": texts, "model": e.model, "dimensions": e.dims}
	case "ollama":
		body = map[string]any{"input": texts, "model": e.model}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("%s embeddings: HTTP %d: %.200s", e.provider, resp.StatusCode, errBody.String())
	}

	if e.provider == "ollama" {
		var out struct {
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return normalizeAll(out.Embeddings), nil
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return normalizeAll(vecs), nil
}

// normalizeAll L2-normalizes vectors so dot product equals cosine similarity.
func normalizeAll(vecs [][]float32) [][]float32 {
	for _, v := range vecs {
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		if sum == 0 {
			continue
		}
		inv := float32(1.0 / math.Sqrt(sum))
		for i := range v {
			v[i] *= inv
		}
	}
	return vecs
}

func apiKey(cfg config.EmbeddingsConfig, defaultEnv string) string {
	env := cfg.APIKeyEnv
	if env == "" {
		env = defaultEnv
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	// Fall back to ~/.ox/secrets.env — daemons, tmux windows, and hooks don't
	// reliably inherit shell-sourced env.
	return secretsEnv(env)
}

func secretsEnv(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(home + "/.ox/secrets.env")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if val, ok := strings.CutPrefix(line, name+"="); ok {
			return strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return ""
}

func defaultStr(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

func defaultInt(v, d int) int {
	if v != 0 {
		return v
	}
	return d
}
