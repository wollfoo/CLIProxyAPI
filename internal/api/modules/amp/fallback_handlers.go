package amp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httputil"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

// FallbackHandler wraps a standard handler with fallback logic to ampcode.com
// when the model's provider is not available in CLIProxyAPI
type FallbackHandler struct {
	getProxy func() *httputil.ReverseProxy
	getConfig func() *config.Config // Thêm để check claude-api-key aliases
}

// NewFallbackHandler creates a new fallback handler wrapper
// The getProxy function allows lazy evaluation of the proxy (useful when proxy is created after routes)
func NewFallbackHandler(getProxy func() *httputil.ReverseProxy) *FallbackHandler {
	return &FallbackHandler{
		getProxy: getProxy,
	}
}

// NewFallbackHandlerWithConfig creates a new fallback handler wrapper with config access
// Cho phép check claude-api-key aliases để ép dùng Azure Claude thay vì fallback
func NewFallbackHandlerWithConfig(getProxy func() *httputil.ReverseProxy, getConfig func() *config.Config) *FallbackHandler {
	return &FallbackHandler{
		getProxy:  getProxy,
		getConfig: getConfig,
	}
}

// WrapHandler wraps a gin.HandlerFunc with fallback logic
// If the model's provider is not configured in CLIProxyAPI, it forwards to ampcode.com
func (fh *FallbackHandler) WrapHandler(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read the request body to extract the model name
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Errorf("amp fallback: failed to read request body: %v", err)
			handler(c)
			return
		}

		// Restore the body for the handler to read
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Try to extract model from request body or URL path (for Gemini)
		modelName := extractModelFromRequest(bodyBytes, c)
		if modelName == "" {
			// Can't determine model, proceed with normal handler
			handler(c)
			return
		}

		// Normalize model (handles Gemini thinking suffixes)
		normalizedModel, _ := util.NormalizeGeminiThinkingModel(modelName)

		// Check if we have providers for this model
		providers := util.GetProviderName(normalizedModel)

		// [AZURE-CLAUDE] Check thêm: nếu model match với claude-api-key aliases, coi như có provider
		if len(providers) == 0 && fh.hasClaudeAPIKeyAlias(modelName) {
			log.Infof("amp fallback: model %s matched claude-api-key alias, using local provider", modelName)
			providers = []string{"claude"}
		}

		if len(providers) == 0 {
			// No providers configured - check if we have a proxy for fallback
			proxy := fh.getProxy()
			if proxy != nil {
				// Fallback to ampcode.com
				log.Infof("amp fallback: model %s has no configured provider, forwarding to ampcode.com", modelName)

				// Restore body again for the proxy
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

				// Forward to ampcode.com
				proxy.ServeHTTP(c.Writer, c.Request)
				return
			}

			// No proxy available, let the normal handler return the error
			log.Debugf("amp fallback: model %s has no configured provider and no proxy available", modelName)
		}

		// Providers available or no proxy for fallback, restore body and use normal handler
		// Filter Anthropic-Beta header to remove features requiring special subscription
		// This is needed when using local providers (bypassing the Amp proxy)
		if betaHeader := c.Request.Header.Get("Anthropic-Beta"); betaHeader != "" {
			filtered := filterBetaFeatures(betaHeader, "context-1m-2025-08-07")
			if filtered != "" {
				c.Request.Header.Set("Anthropic-Beta", filtered)
			} else {
				c.Request.Header.Del("Anthropic-Beta")
			}
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		handler(c)
	}
}

// extractModelFromRequest attempts to extract the model name from various request formats
func extractModelFromRequest(body []byte, c *gin.Context) string {
	// First try to parse from JSON body (OpenAI, Claude, etc.)
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		// Check common model field names
		if model, ok := payload["model"].(string); ok {
			return model
		}
	}

	// For Gemini requests, model is in the URL path
	// Standard format: /models/{model}:generateContent -> :action parameter
	if action := c.Param("action"); action != "" {
		// Split by colon to get model name (e.g., "gemini-pro:generateContent" -> "gemini-pro")
		parts := strings.Split(action, ":")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}

	// AMP CLI format: /publishers/google/models/{model}:method -> *path parameter
	// Example: /publishers/google/models/gemini-3-pro-preview:streamGenerateContent
	if path := c.Param("path"); path != "" {
		// Look for /models/{model}:method pattern
		if idx := strings.Index(path, "/models/"); idx >= 0 {
			modelPart := path[idx+8:] // Skip "/models/"
			// Split by colon to get model name
			if colonIdx := strings.Index(modelPart, ":"); colonIdx > 0 {
				return modelPart[:colonIdx]
			}
		}
	}

	return ""
}

// hasClaudeAPIKeyAlias checks if the model name matches any alias in claude-api-key config
// Hàm này kiểm tra xem model có match với aliases trong config.yaml không
// Ví dụ: claude-haiku-4-5-20251001 có thể được map sang claude-sonnet-4-20250514 (Azure)
func (fh *FallbackHandler) hasClaudeAPIKeyAlias(modelName string) bool {
	if fh.getConfig == nil {
		return false
	}
	cfg := fh.getConfig()
	if cfg == nil || len(cfg.ClaudeKey) == 0 {
		return false
	}

	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return false
	}

	// Check từng claude-api-key entry
	for _, ck := range cfg.ClaudeKey {
		// Nếu có models aliases được định nghĩa
		if len(ck.Models) > 0 {
			for _, model := range ck.Models {
				alias := strings.ToLower(strings.TrimSpace(model.Alias))
				name := strings.ToLower(strings.TrimSpace(model.Name))
				// Match nếu alias hoặc name trùng với model được request
				if alias != "" && alias == modelLower {
					return true
				}
				if name != "" && name == modelLower {
					return true
				}
			}
		} else {
			// Nếu không có models aliases, check xem có base-url và api-key không
			// Có nghĩa là đây là Claude API key config, có thể xử lý mọi Claude model
			if strings.TrimSpace(ck.BaseURL) != "" && strings.TrimSpace(ck.APIKey) != "" {
				// Nếu model bắt đầu bằng "claude-", coi như có thể xử lý
				if strings.HasPrefix(modelLower, "claude-") {
					return true
				}
			}
		}
	}

	return false
}

