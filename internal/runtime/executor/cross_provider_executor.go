package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/sjson"
)

// CrossProviderExecutor handles cross-provider routing with protocol translation.
// It translates requests from source format (e.g., OpenAI) to target format (e.g., Claude)
// and vice versa for responses, enabling model aliasing across different API providers.
//
// Example use case: Route Oracle's GPT-5 requests to Azure AI Foundry Claude
// - Source: OpenAI Chat Completions API (model: gpt-5)
// - Target: Anthropic Messages API (model: claude-opus-4-5)
type CrossProviderExecutor struct {
	cfg          *config.Config
	providerType string // "claude", "gemini", etc.
}

// NewCrossProviderExecutor creates a new cross-provider executor.
// providerType determines which backend API format to use (e.g., "claude" for Anthropic).
func NewCrossProviderExecutor(cfg *config.Config, providerType string) *CrossProviderExecutor {
	return &CrossProviderExecutor{
		cfg:          cfg,
		providerType: strings.ToLower(strings.TrimSpace(providerType)),
	}
}

// Identifier returns the executor identifier for logging and metrics.
func (e *CrossProviderExecutor) Identifier() string {
	return "cross-provider-" + e.providerType
}

// PrepareRequest is a no-op for cross-provider routing.
func (e *CrossProviderExecutor) PrepareRequest(_ *http.Request, _ *cliproxyauth.Auth) error {
	return nil
}

// Execute handles non-streaming requests with protocol translation.
// Flow: OpenAI Request → Translate → Claude Request → Execute → Claude Response → Translate → OpenAI Response
func (e *CrossProviderExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	switch e.providerType {
	case "claude":
		return e.executeWithClaude(ctx, auth, req, opts)
	default:
		return resp, fmt.Errorf("cross-provider executor: unsupported provider type: %s", e.providerType)
	}
}

// ExecuteStream handles streaming requests with protocol translation.
func (e *CrossProviderExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	switch e.providerType {
	case "claude":
		return e.executeStreamWithClaude(ctx, auth, req, opts)
	default:
		return nil, fmt.Errorf("cross-provider executor: unsupported provider type: %s", e.providerType)
	}
}

// CountTokens delegates to the appropriate token counting implementation.
func (e *CrossProviderExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	switch e.providerType {
	case "claude":
		return e.countTokensWithClaude(ctx, auth, req, opts)
	default:
		return cliproxyexecutor.Response{}, fmt.Errorf("cross-provider executor: unsupported provider type: %s", e.providerType)
	}
}

// Refresh is a no-op for API-key based cross-provider routing.
func (e *CrossProviderExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("cross-provider executor: refresh called for %s", e.providerType)
	return auth, nil
}

// =============================================================================
// Claude Backend Implementation
// =============================================================================

// executeWithClaude handles non-streaming requests to Claude backend.
func (e *CrossProviderExecutor) executeWithClaude(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	apiKey, baseURL := crossProviderCreds(auth)
	if baseURL == "" {
		return resp, statusErr{code: http.StatusUnauthorized, msg: "cross-provider executor: missing base URL"}
	}
	if apiKey == "" {
		return resp, statusErr{code: http.StatusUnauthorized, msg: "cross-provider executor: missing API key"}
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	// Step 1: Translate request from OpenAI to Claude format
	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")

	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)

	// Resolve model alias to upstream model name
	if modelOverride := e.resolveUpstreamModel(req.Model, auth); modelOverride != "" {
		body, _ = sjson.SetBytes(body, "model", modelOverride)
		log.Debugf("cross-provider executor: model alias %s → %s", req.Model, modelOverride)
	}

	// [AZURE-FIX] Sanitize tool names for Azure Foundry compatibility
	body = sanitizeToolNames(body)

	// Apply payload config
	body = applyPayloadConfig(e.cfg, req.Model, body)

	// Step 2: Execute request to Claude endpoint
	url := strings.TrimSuffix(baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}

	// Apply Claude headers
	applyCrossProviderClaudeHeaders(httpReq, auth, apiKey)

	// Log request
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	// Execute HTTP request
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("cross-provider executor: close response body error: %v", errClose)
		}
	}()

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		log.Debugf("cross-provider executor: request error, status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return resp, err
	}

	// Read response
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	// Parse usage from Claude response
	detail := parseClaudeUsage(data)
	if detail.TotalTokens > 0 {
		reporter.publish(ctx, detail)
	}

	// Step 3: Translate response from Claude to OpenAI format
	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, data, &param)

	resp = cliproxyexecutor.Response{Payload: []byte(out)}
	return resp, nil
}

// executeStreamWithClaude handles streaming requests to Claude backend.
func (e *CrossProviderExecutor) executeStreamWithClaude(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	apiKey, baseURL := crossProviderCreds(auth)
	if baseURL == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "cross-provider executor: missing base URL"}
	}
	if apiKey == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "cross-provider executor: missing API key"}
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)

	// Step 1: Translate request from OpenAI to Claude format
	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")

	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), true)

	// Resolve model alias
	if modelOverride := e.resolveUpstreamModel(req.Model, auth); modelOverride != "" {
		body, _ = sjson.SetBytes(body, "model", modelOverride)
		log.Debugf("cross-provider executor: model alias %s → %s", req.Model, modelOverride)
	}

	// Enable streaming
	body, _ = sjson.SetBytes(body, "stream", true)

	// [AZURE-FIX] Sanitize tool names
	body = sanitizeToolNames(body)

	// Apply payload config
	body = applyPayloadConfig(e.cfg, req.Model, body)

	// Step 2: Execute request to Claude endpoint
	url := strings.TrimSuffix(baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Apply headers
	applyCrossProviderClaudeHeaders(httpReq, auth, apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Log request
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	// Execute HTTP request
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		reporter.publishFailure(ctx)
		return nil, err
	}

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, readErr := io.ReadAll(httpResp.Body)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("cross-provider executor: close response body error: %v", errClose)
		}
		if readErr != nil {
			recordAPIResponseError(ctx, e.cfg, readErr)
			reporter.publishFailure(ctx)
			return nil, readErr
		}
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("cross-provider executor: request error, status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		reporter.publishFailure(ctx)
		return nil, statusErr{code: httpResp.StatusCode, msg: string(data)}
	}

	// Step 3: Stream response with translation
	out := make(chan cliproxyexecutor.StreamChunk)

	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("cross-provider executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 20_971_520)
		var param any

		for scanner.Scan() {
			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)

			// Parse usage from streaming chunks
			if detail, ok := parseClaudeStreamUsage(line); ok {
				reporter.publish(ctx, detail)
			}

			// Translate each chunk from Claude to OpenAI format
			chunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, bytes.Clone(line), &param)
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}

		// Ensure usage is published even if no usage chunk was received
		reporter.ensurePublished(ctx)
	}()

	return out, nil
}

// countTokensWithClaude handles token counting for Claude backend.
func (e *CrossProviderExecutor) countTokensWithClaude(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")

	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)

	// Resolve model alias
	modelForCounting := req.Model
	if modelOverride := e.resolveUpstreamModel(req.Model, auth); modelOverride != "" {
		body, _ = sjson.SetBytes(body, "model", modelOverride)
		modelForCounting = modelOverride
	}

	// Use Claude tokenizer
	enc, err := tokenizerForModel(modelForCounting)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("cross-provider executor: tokenizer init failed: %w", err)
	}

	count, err := countOpenAIChatTokens(enc, body)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("cross-provider executor: token counting failed: %w", err)
	}

	usageJSON := buildOpenAIUsageJSON(count)
	translatedUsage := sdktranslator.TranslateTokenCount(ctx, to, from, count, usageJSON)
	return cliproxyexecutor.Response{Payload: []byte(translatedUsage)}, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// resolveUpstreamModel resolves model alias to upstream model name from codex-api-key config.
func (e *CrossProviderExecutor) resolveUpstreamModel(alias string, auth *cliproxyauth.Auth) string {
	if alias == "" || auth == nil || e.cfg == nil {
		return ""
	}

	// Get provider key from auth attributes
	var providerKey string
	if auth.Attributes != nil {
		providerKey = strings.TrimSpace(auth.Attributes["provider_key"])
	}
	if providerKey == "" {
		return ""
	}

	// Find matching codex-api-key config
	for i := range e.cfg.CodexKey {
		ck := &e.cfg.CodexKey[i]

		// Check if this is the matching config (by base URL or provider key)
		if auth.Attributes != nil {
			if baseURL := strings.TrimSpace(auth.Attributes["base_url"]); baseURL != "" {
				if strings.TrimSpace(ck.BaseURL) != baseURL {
					continue
				}
			}
		}

		// Check if provider type matches
		if !strings.EqualFold(ck.ProviderType, e.providerType) {
			continue
		}

		// Search for alias in models
		for j := range ck.Models {
			model := &ck.Models[j]
			if strings.EqualFold(strings.TrimSpace(model.Alias), alias) {
				if model.Name != "" {
					return model.Name
				}
				return alias
			}
		}
	}

	return ""
}

// crossProviderCreds extracts API key and base URL from auth.
func crossProviderCreds(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil || auth.Attributes == nil {
		return "", ""
	}
	apiKey = strings.TrimSpace(auth.Attributes["api_key"])
	baseURL = strings.TrimSpace(auth.Attributes["base_url"])
	return
}

// applyCrossProviderClaudeHeaders applies headers for Claude API requests.
func applyCrossProviderClaudeHeaders(r *http.Request, auth *cliproxyauth.Auth, apiKey string) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-api-key", apiKey)
	r.Header.Set("anthropic-version", "2023-06-01")

	// Apply custom headers from auth attributes
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(r, attrs)
}
