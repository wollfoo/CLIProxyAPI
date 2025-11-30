package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/tidwall/gjson"
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

	// [CLAUDE-FIX] Extract system messages from messages array to top-level system parameter
	body = extractSystemToTopLevel(body)

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

	// [CLAUDE-FIX] Extract system messages from messages array to top-level system parameter
	body = extractSystemToTopLevel(body)

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
			// Check if context was cancelled (client disconnected)
			if ctx.Err() != nil {
				log.Debugf("cross-provider executor: context cancelled, stopping stream")
				break
			}

			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)

			// Parse usage from streaming chunks
			if detail, ok := parseClaudeStreamUsage(line); ok {
				reporter.publish(ctx, detail)
			}

			// Translate each chunk from Claude to OpenAI format
			chunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, bytes.Clone(line), &param)
			if len(chunks) > 0 {
				preview := string(line)
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				log.Debugf("cross-provider executor: translated %d chunks from: %s", len(chunks), preview)
			}
			for i := range chunks {
				// Log the actual translated event being sent
				if len(chunks[i]) > 0 {
					eventPreview := chunks[i]
					if len(eventPreview) > 150 {
						eventPreview = eventPreview[:150] + "..."
					}
					log.Debugf("cross-provider executor: sending event: %s", eventPreview)
				}
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			log.Errorf("cross-provider executor: scanner error: %v", errScan)
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		} else {
			log.Debugf("cross-provider executor: stream completed normally")
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

	// [CLAUDE-FIX] Extract system messages from messages array to top-level system parameter
	body = extractSystemToTopLevel(body)

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

// resolveUpstreamModel resolves model alias to upstream model name from auth attributes.
// The model_name is stored in auth attributes when the cross-provider auth is created.
func (e *CrossProviderExecutor) resolveUpstreamModel(alias string, auth *cliproxyauth.Auth) string {
	if alias == "" || auth == nil || auth.Attributes == nil {
		return ""
	}

	// Get model_name directly from auth attributes (set during auth synthesis)
	modelName := strings.TrimSpace(auth.Attributes["model_name"])
	if modelName != "" {
		return modelName
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

// extractSystemToTopLevel extracts system messages from messages array and moves them to top-level system parameter.
// This is required for Claude API which expects system to be a top-level parameter, not a message role.
func extractSystemToTopLevel(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	var systemParts []map[string]interface{}
	var filteredMessages []json.RawMessage

	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		if role == "system" {
			// Extract system message content
			content := msg.Get("content")
			if content.Type == gjson.String && content.String() != "" {
				systemParts = append(systemParts, map[string]interface{}{
					"type": "text",
					"text": content.String(),
				})
			} else if content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					if part.Get("type").String() == "text" {
						systemParts = append(systemParts, map[string]interface{}{
							"type": "text",
							"text": part.Get("text").String(),
						})
					}
					return true
				})
			}
		} else {
			// Keep non-system messages
			filteredMessages = append(filteredMessages, json.RawMessage(msg.Raw))
		}
		return true
	})

	// If we found system messages, update the body
	if len(systemParts) > 0 {
		// Set top-level system parameter
		systemJSON, _ := json.Marshal(systemParts)
		body, _ = sjson.SetRawBytes(body, "system", systemJSON)

		// Replace messages with filtered messages (without system)
		messagesJSON, _ := json.Marshal(filteredMessages)
		body, _ = sjson.SetRawBytes(body, "messages", messagesJSON)

		log.Debugf("cross-provider executor: extracted %d system parts to top-level", len(systemParts))
	}

	return body
}
