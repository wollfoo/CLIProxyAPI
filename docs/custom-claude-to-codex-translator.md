# Custom Claude-to-Codex Translator

Hướng dẫn kỹ thuật nâng cao để xây dựng translator chuyển đổi request từ Claude API sang Codex (GPT-5.1) trong CLIProxyAPI, cho phép Amp CLI sử dụng GPT-5.1 làm model Smart mặc định.

## Mục lục

- [Tổng quan](#tổng-quan)
- [Kiến trúc](#kiến-trúc)
- [Yêu cầu](#yêu-cầu)
- [Triển khai](#triển-khai)
  - [Bước 1: Tạo Translator Module](#bước-1-tạo-translator-module)
  - [Bước 2: Đăng ký Translator](#bước-2-đăng-ký-translator)
  - [Bước 3: Tạo Custom Handler](#bước-3-tạo-custom-handler)
  - [Bước 4: Cấu hình Model Routing](#bước-4-cấu-hình-model-routing)
- [Cấu hình](#cấu-hình)
- [Testing](#testing)
- [Troubleshooting](#troubleshooting)
- [Tham khảo](#tham-khảo)

---

## Tổng quan

### ⚠️ Quan trọng: Ảnh hưởng đến luồng chính

**Giải pháp này được thiết kế để KHÔNG ảnh hưởng đến luồng chính của CLIProxyAPI:**

| Khía cạnh | Thiết kế | Giải thích |
|-----------|----------|------------|
| **Code location** | Module riêng biệt | Tất cả code mới nằm trong `internal/translator/claude_to_codex/`, không sửa code gốc |
| **Config toggle** | Bật/tắt qua config | `claude-to-codex-intercept: false` → luồng gốc 100% |
| **Selective intercept** | Chỉ model trong mapping | Model không trong mapping → đi thẳng Claude handler |
| **Error fallback** | Tự động fallback | Nếu translator lỗi → request đi Claude handler |
| **Runtime toggle** | Có thể thay đổi | Thay đổi config và hot-reload, không cần restart |

### Luồng request khi BẬT và TẮT

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           REQUEST FLOW COMPARISON                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  [claude-to-codex-intercept: FALSE]  │  [claude-to-codex-intercept: TRUE]   │
│  ═══════════════════════════════════  │  ══════════════════════════════════  │
│                                       │                                      │
│  Amp CLI                              │  Amp CLI                             │
│      ↓                                │      ↓                               │
│  /api/provider/anthropic/v1/messages  │  /api/provider/anthropic/v1/messages │
│      ↓                                │      ↓                               │
│  Claude Handler (DIRECT)              │  Interceptor                         │
│      ↓                                │      ↓                               │
│  Azure Claude API                     │  Model in mapping?                   │
│      ↓                                │      ↓ YES          ↓ NO             │
│  Response                             │  Translate →     Claude Handler      │
│                                       │  Codex (GPT-5.1)     ↓               │
│                                       │      ↓           Azure Claude        │
│                                       │  Translate back      ↓               │
│                                       │      ↓           Response            │
│                                       │  Response                            │
│                                       │                                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Vấn đề

Amp CLI mặc định sử dụng **Claude Opus 4.5** cho Smart mode. Khi sử dụng CLIProxyAPI, các request được gửi đến:

```
POST /api/provider/anthropic/v1/messages
```

Mục tiêu là chuyển hướng các request này sang **GPT-5.1 (Codex)** thay vì Azure Claude.

### Thách thức kỹ thuật

| Thành phần | Claude API | Codex/OpenAI API |
|------------|------------|------------------|
| Endpoint | `/v1/messages` | `/v1/chat/completions` |
| Request format | Anthropic Messages | OpenAI Chat Completions |
| Response format | Anthropic Response | OpenAI Response |
| Streaming | SSE với `event: content_block_delta` | SSE với `data: {...}` |

### Giải pháp

Xây dựng một **translator layer** trong CLIProxyAPI:

```
Amp CLI (Claude request)
    ↓
Claude-to-Codex Translator
    ↓
Codex Executor (GPT-5.1)
    ↓
Codex-to-Claude Translator
    ↓
Amp CLI (Claude response)
```

---

## Kiến trúc

### Tổng quan hệ thống

```
┌─────────────────────────────────────────────────────────────────┐
│                         Amp CLI                                  │
│                    (Claude format)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    CLIProxyAPI Server                            │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │            /api/provider/anthropic/v1/messages            │  │
│  │                   (Amp Route Alias)                       │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              ▼                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │              Claude-to-Codex Interceptor                  │  │
│  │  ┌─────────────────┐    ┌─────────────────────────────┐  │  │
│  │  │ Model Matcher   │───▶│ Request Translator          │  │  │
│  │  │ (claude-opus-*) │    │ (Claude → OpenAI format)    │  │  │
│  │  └─────────────────┘    └─────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              ▼                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                   Codex Executor                          │  │
│  │              (GPT-5.1 via OAuth token)                    │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              ▼                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │              Codex-to-Claude Translator                   │  │
│  │           (OpenAI response → Claude format)               │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Amp CLI                                  │
│                  (Claude response format)                        │
└─────────────────────────────────────────────────────────────────┘
```

### Các thành phần chính

1. **Model Matcher**: Xác định model Claude nào cần được redirect sang Codex
2. **Request Translator**: Chuyển đổi Claude Messages API → OpenAI Chat Completions API
3. **Codex Executor**: Gọi GPT-5.1 thông qua OAuth token đã đăng nhập
4. **Response Translator**: Chuyển đổi OpenAI response → Claude response format

---

## Yêu cầu

### Môi trường

- Go 1.24+
- CLIProxyAPI v6.x source code
- Codex OAuth token (đã đăng nhập qua `./cli-proxy-api --codex-login`)

### Kiến thức cần thiết

- Go programming
- Anthropic Messages API format
- OpenAI Chat Completions API format
- Server-Sent Events (SSE) streaming

---

## Triển khai

### Bước 1: Tạo Translator Module

Tạo file mới: `internal/translator/claude_to_codex/translator.go`

```go
package claude_to_codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ========================================
// CLAUDE API TYPES (Input)
// ========================================

// ClaudeRequest represents the Anthropic Messages API request format
type ClaudeRequest struct {
	Model       string           `json:"model"`
	Messages    []ClaudeMessage  `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	TopK        *int             `json:"top_k,omitempty"`
	StopSeqs    []string         `json:"stop_sequences,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	System      interface{}      `json:"system,omitempty"` // string or []ContentBlock
	Metadata    *ClaudeMetadata  `json:"metadata,omitempty"`
	Tools       []ClaudeTool     `json:"tools,omitempty"`
	ToolChoice  interface{}      `json:"tool_choice,omitempty"`
	Thinking    *ClaudeThinking  `json:"thinking,omitempty"`
}

type ClaudeMessage struct {
	Role    string      `json:"role"` // "user" or "assistant"
	Content interface{} `json:"content"` // string or []ContentBlock
}

type ClaudeContentBlock struct {
	Type      string                 `json:"type"` // "text", "image", "tool_use", "tool_result", "thinking"
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`
}

type ClaudeMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type ClaudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type ClaudeThinking struct {
	Type         string `json:"type,omitempty"`          // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // thinking budget
}

// ========================================
// OPENAI API TYPES (Output for Codex)
// ========================================

// OpenAIRequest represents the OpenAI Chat Completions API request format
type OpenAIRequest struct {
	Model            string              `json:"model"`
	Messages         []OpenAIMessage     `json:"messages"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Temperature      *float64            `json:"temperature,omitempty"`
	TopP             *float64            `json:"top_p,omitempty"`
	Stop             []string            `json:"stop,omitempty"`
	Stream           bool                `json:"stream,omitempty"`
	StreamOptions    *StreamOptions      `json:"stream_options,omitempty"`
	Tools            []OpenAITool        `json:"tools,omitempty"`
	ToolChoice       interface{}         `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat     `json:"response_format,omitempty"`
	Reasoning        *ReasoningConfig    `json:"reasoning,omitempty"`
}

type OpenAIMessage struct {
	Role       string        `json:"role"` // "system", "user", "assistant", "tool"
	Content    interface{}   `json:"content"` // string or []ContentPart
	Name       string        `json:"name,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"` // "text", "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type OpenAITool struct {
	Type     string         `json:"type"` // "function"
	Function OpenAIFunction `json:"function"`
}

type OpenAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type ResponseFormat struct {
	Type string `json:"type"` // "text", "json_object"
}

type ReasoningConfig struct {
	Effort string `json:"effort,omitempty"` // "low", "medium", "high"
}

// ========================================
// TRANSLATOR FUNCTIONS
// ========================================

// ModelMapping defines Claude to Codex model mappings
var ModelMapping = map[string]string{
	"claude-opus-4-5-20251101": "gpt-5.1",
	"claude-opus-4-5":          "gpt-5.1",
	"claude-opus-4-1":          "gpt-5.1",
	"claude-sonnet-4-5":        "gpt-5.1-mini",
	"claude-sonnet-4-5-20250929": "gpt-5.1-mini",
	"claude-haiku-4-5":         "gpt-5-turbo",
	"claude-haiku-4-5-20251001": "gpt-5-turbo",
}

// ShouldIntercept checks if a model should be intercepted and redirected to Codex
func ShouldIntercept(model string) bool {
	_, exists := ModelMapping[model]
	return exists
}

// GetCodexModel returns the corresponding Codex model for a Claude model
func GetCodexModel(claudeModel string) string {
	if codexModel, exists := ModelMapping[claudeModel]; exists {
		return codexModel
	}
	return "gpt-5.1" // Default fallback
}

// TranslateRequest converts a Claude Messages API request to OpenAI Chat Completions format
func TranslateRequest(claudeReq *ClaudeRequest) (*OpenAIRequest, error) {
	openaiReq := &OpenAIRequest{
		Model:       GetCodexModel(claudeReq.Model),
		MaxTokens:   claudeReq.MaxTokens,
		Temperature: claudeReq.Temperature,
		TopP:        claudeReq.TopP,
		Stop:        claudeReq.StopSeqs,
		Stream:      claudeReq.Stream,
	}

	// Enable stream options for usage tracking
	if claudeReq.Stream {
		openaiReq.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	// Convert system message
	messages := []OpenAIMessage{}
	if claudeReq.System != nil {
		systemContent := extractSystemContent(claudeReq.System)
		if systemContent != "" {
			messages = append(messages, OpenAIMessage{
				Role:    "system",
				Content: systemContent,
			})
		}
	}

	// Convert messages
	for _, msg := range claudeReq.Messages {
		openaiMsg, err := translateMessage(msg)
		if err != nil {
			return nil, err
		}
		messages = append(messages, openaiMsg...)
	}
	openaiReq.Messages = messages

	// Convert tools
	if len(claudeReq.Tools) > 0 {
		openaiReq.Tools = translateTools(claudeReq.Tools)
		if claudeReq.ToolChoice != nil {
			openaiReq.ToolChoice = translateToolChoice(claudeReq.ToolChoice)
		}
	}

	// Handle thinking/reasoning
	if claudeReq.Thinking != nil && claudeReq.Thinking.Type == "enabled" {
		openaiReq.Reasoning = &ReasoningConfig{Effort: "high"}
	}

	return openaiReq, nil
}

func extractSystemContent(system interface{}) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if text, exists := block["text"].(string); exists {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func translateMessage(msg ClaudeMessage) ([]OpenAIMessage, error) {
	var messages []OpenAIMessage

	role := msg.Role
	if role == "user" || role == "assistant" {
		// Valid role
	} else {
		role = "user" // Default fallback
	}

	switch content := msg.Content.(type) {
	case string:
		messages = append(messages, OpenAIMessage{
			Role:    role,
			Content: content,
		})

	case []interface{}:
		// Handle content blocks
		var textParts []string
		var toolCalls []ToolCall
		var toolResults []OpenAIMessage

		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			blockType, _ := block["type"].(string)

			switch blockType {
			case "text":
				if text, exists := block["text"].(string); exists {
					textParts = append(textParts, text)
				}

			case "thinking":
				// Skip thinking blocks (internal to Claude)
				continue

			case "tool_use":
				// Convert to OpenAI tool_call
				tc := ToolCall{
					ID:   block["id"].(string),
					Type: "function",
					Function: FunctionCall{
						Name: block["name"].(string),
					},
				}
				if input, exists := block["input"].(map[string]interface{}); exists {
					inputJSON, _ := json.Marshal(input)
					tc.Function.Arguments = string(inputJSON)
				}
				toolCalls = append(toolCalls, tc)

			case "tool_result":
				// Convert to OpenAI tool message
				toolMsg := OpenAIMessage{
					Role:       "tool",
					ToolCallID: block["tool_use_id"].(string),
				}
				if resultContent, exists := block["content"]; exists {
					switch rc := resultContent.(type) {
					case string:
						toolMsg.Content = rc
					case []interface{}:
						var texts []string
						for _, c := range rc {
							if cb, ok := c.(map[string]interface{}); ok {
								if t, exists := cb["text"].(string); exists {
									texts = append(texts, t)
								}
							}
						}
						toolMsg.Content = strings.Join(texts, "\n")
					}
				}
				toolResults = append(toolResults, toolMsg)
			}
		}

		// Build main message
		if len(textParts) > 0 || len(toolCalls) > 0 {
			mainMsg := OpenAIMessage{
				Role:    role,
				Content: strings.Join(textParts, "\n"),
			}
			if len(toolCalls) > 0 {
				mainMsg.ToolCalls = toolCalls
			}
			messages = append(messages, mainMsg)
		}

		// Add tool results as separate messages
		messages = append(messages, toolResults...)
	}

	return messages, nil
}

func translateTools(claudeTools []ClaudeTool) []OpenAITool {
	var openaiTools []OpenAITool
	for _, ct := range claudeTools {
		openaiTools = append(openaiTools, OpenAITool{
			Type: "function",
			Function: OpenAIFunction{
				Name:        ct.Name,
				Description: ct.Description,
				Parameters:  ct.InputSchema,
			},
		})
	}
	return openaiTools
}

func translateToolChoice(claudeChoice interface{}) interface{} {
	switch v := claudeChoice.(type) {
	case string:
		switch v {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "none":
			return "none"
		}
	case map[string]interface{}:
		if v["type"] == "tool" {
			if name, exists := v["name"].(string); exists {
				return map[string]interface{}{
					"type": "function",
					"function": map[string]string{
						"name": name,
					},
				}
			}
		}
	}
	return "auto"
}

// ========================================
// RESPONSE TRANSLATION
// ========================================

// ClaudeResponse represents the Anthropic Messages API response format
type ClaudeResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"` // "message"
	Role         string               `json:"role"` // "assistant"
	Content      []ClaudeContentBlock `json:"content"`
	Model        string               `json:"model"`
	StopReason   string               `json:"stop_reason,omitempty"`
	StopSequence *string              `json:"stop_sequence,omitempty"`
	Usage        ClaudeUsage          `json:"usage"`
}

type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// OpenAIResponse represents the OpenAI Chat Completions API response format
type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      OpenAIMessage  `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// TranslateResponse converts an OpenAI response to Claude format
func TranslateResponse(openaiResp *OpenAIResponse, originalClaudeModel string) *ClaudeResponse {
	claudeResp := &ClaudeResponse{
		ID:    fmt.Sprintf("msg_%s", strings.ReplaceAll(openaiResp.ID, "chatcmpl-", "")),
		Type:  "message",
		Role:  "assistant",
		Model: originalClaudeModel,
	}

	// Convert content
	var contentBlocks []ClaudeContentBlock
	if len(openaiResp.Choices) > 0 {
		choice := openaiResp.Choices[0]
		msg := choice.Message
		if choice.Delta != nil {
			msg = *choice.Delta
		}

		// Text content
		if content, ok := msg.Content.(string); ok && content != "" {
			contentBlocks = append(contentBlocks, ClaudeContentBlock{
				Type: "text",
				Text: content,
			})
		}

		// Tool calls
		for _, tc := range msg.ToolCalls {
			var input map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			contentBlocks = append(contentBlocks, ClaudeContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		// Stop reason
		claudeResp.StopReason = translateStopReason(choice.FinishReason)
	}
	claudeResp.Content = contentBlocks

	// Usage
	if openaiResp.Usage != nil {
		claudeResp.Usage = ClaudeUsage{
			InputTokens:  openaiResp.Usage.PromptTokens,
			OutputTokens: openaiResp.Usage.CompletionTokens,
		}
	}

	return claudeResp
}

func translateStopReason(openaiReason string) string {
	switch openaiReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// ========================================
// STREAMING TRANSLATION
// ========================================

// ClaudeStreamEvent represents a Claude SSE event
type ClaudeStreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`
	Delta json.RawMessage `json:"delta,omitempty"`
	// For message_start
	Message *ClaudeResponse `json:"message,omitempty"`
	// For content_block_start
	ContentBlock *ClaudeContentBlock `json:"content_block,omitempty"`
	// For message_delta
	Usage *ClaudeUsage `json:"usage,omitempty"`
}

// StreamTranslator handles streaming response translation
type StreamTranslator struct {
	originalModel string
	messageID     string
	blockIndex    int
	started       bool
}

// NewStreamTranslator creates a new stream translator
func NewStreamTranslator(originalModel string) *StreamTranslator {
	return &StreamTranslator{
		originalModel: originalModel,
		messageID:     fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
		blockIndex:    0,
		started:       false,
	}
}

// TranslateStreamChunk converts an OpenAI stream chunk to Claude stream events
func (st *StreamTranslator) TranslateStreamChunk(chunk []byte) ([][]byte, error) {
	var openaiChunk OpenAIResponse
	if err := json.Unmarshal(chunk, &openaiChunk); err != nil {
		return nil, err
	}

	var events [][]byte

	// First chunk: send message_start
	if !st.started {
		st.started = true
		startEvent := ClaudeStreamEvent{
			Type: "message_start",
			Message: &ClaudeResponse{
				ID:      st.messageID,
				Type:    "message",
				Role:    "assistant",
				Model:   st.originalModel,
				Content: []ClaudeContentBlock{},
				Usage:   ClaudeUsage{InputTokens: 0, OutputTokens: 0},
			},
		}
		eventBytes, _ := json.Marshal(startEvent)
		events = append(events, formatSSE("message_start", eventBytes))

		// content_block_start for text
		blockStartEvent := ClaudeStreamEvent{
			Type:  "content_block_start",
			Index: 0,
			ContentBlock: &ClaudeContentBlock{
				Type: "text",
				Text: "",
			},
		}
		blockBytes, _ := json.Marshal(blockStartEvent)
		events = append(events, formatSSE("content_block_start", blockBytes))
	}

	// Process choices
	for _, choice := range openaiChunk.Choices {
		if choice.Delta != nil {
			if content, ok := choice.Delta.Content.(string); ok && content != "" {
				// content_block_delta
				deltaEvent := map[string]interface{}{
					"type":  "content_block_delta",
					"index": st.blockIndex,
					"delta": map[string]string{
						"type": "text_delta",
						"text": content,
					},
				}
				deltaBytes, _ := json.Marshal(deltaEvent)
				events = append(events, formatSSE("content_block_delta", deltaBytes))
			}

			// Handle tool calls in streaming
			for _, tc := range choice.Delta.ToolCalls {
				// Tool use block start
				if tc.Function.Name != "" {
					st.blockIndex++
					toolStartEvent := map[string]interface{}{
						"type":  "content_block_start",
						"index": st.blockIndex,
						"content_block": map[string]interface{}{
							"type":  "tool_use",
							"id":    tc.ID,
							"name":  tc.Function.Name,
							"input": map[string]interface{}{},
						},
					}
					toolBytes, _ := json.Marshal(toolStartEvent)
					events = append(events, formatSSE("content_block_start", toolBytes))
				}
				if tc.Function.Arguments != "" {
					toolDeltaEvent := map[string]interface{}{
						"type":  "content_block_delta",
						"index": st.blockIndex,
						"delta": map[string]string{
							"type":         "input_json_delta",
							"partial_json": tc.Function.Arguments,
						},
					}
					toolDeltaBytes, _ := json.Marshal(toolDeltaEvent)
					events = append(events, formatSSE("content_block_delta", toolDeltaBytes))
				}
			}
		}

		// Handle finish
		if choice.FinishReason != "" {
			// content_block_stop
			blockStopEvent := map[string]interface{}{
				"type":  "content_block_stop",
				"index": st.blockIndex,
			}
			blockStopBytes, _ := json.Marshal(blockStopEvent)
			events = append(events, formatSSE("content_block_stop", blockStopBytes))

			// message_delta
			msgDeltaEvent := map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]string{
					"stop_reason": translateStopReason(choice.FinishReason),
				},
			}
			if openaiChunk.Usage != nil {
				msgDeltaEvent["usage"] = map[string]int{
					"output_tokens": openaiChunk.Usage.CompletionTokens,
				}
			}
			msgDeltaBytes, _ := json.Marshal(msgDeltaEvent)
			events = append(events, formatSSE("message_delta", msgDeltaBytes))

			// message_stop
			stopEvent := map[string]string{"type": "message_stop"}
			stopBytes, _ := json.Marshal(stopEvent)
			events = append(events, formatSSE("message_stop", stopBytes))
		}
	}

	return events, nil
}

func formatSSE(eventType string, data []byte) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
}
```

### Bước 2: Đăng ký Translator

Tạo file: `internal/translator/claude_to_codex/register.go`

```go
package claude_to_codex

import (
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

const (
	FormatClaudeMessages = translator.Format("claude-messages")
	FormatCodexChat      = translator.Format("codex-chat")
)

func init() {
	// Register the Claude-to-Codex translator
	translator.Register(
		FormatClaudeMessages,
		FormatCodexChat,
		// Request transform
		func(model string, rawJSON []byte, stream bool) []byte {
			var claudeReq ClaudeRequest
			if err := json.Unmarshal(rawJSON, &claudeReq); err != nil {
				return rawJSON // Return original on error
			}
			
			openaiReq, err := TranslateRequest(&claudeReq)
			if err != nil {
				return rawJSON
			}
			
			translated, _ := json.Marshal(openaiReq)
			return translated
		},
		// Response transform
		translator.ResponseTransform{
			Stream: func(ctx context.Context, model string, originalReq, translatedReq, raw []byte, param *any) []string {
				// Parse and translate streaming chunks
				st := NewStreamTranslator(model)
				chunks, err := st.TranslateStreamChunk(raw)
				if err != nil {
					return []string{string(raw)}
				}
				
				var result []string
				for _, chunk := range chunks {
					result = append(result, string(chunk))
				}
				return result
			},
			NonStream: func(ctx context.Context, model string, originalReq, translatedReq, raw []byte, param *any) string {
				var openaiResp OpenAIResponse
				if err := json.Unmarshal(raw, &openaiResp); err != nil {
					return string(raw)
				}
				
				claudeResp := TranslateResponse(&openaiResp, model)
				translated, _ := json.Marshal(claudeResp)
				return string(translated)
			},
		},
	)
}
```

### Bước 3: Tạo Custom Handler

Tạo file: `internal/api/handlers/claude_codex_intercept.go`

```go
package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/translator/claude_to_codex"
	log "github.com/sirupsen/logrus"
)

// ClaudeCodexInterceptor intercepts Claude requests and routes to Codex
type ClaudeCodexInterceptor struct {
	codexHandler gin.HandlerFunc
	claudeHandler gin.HandlerFunc
	enabled bool
}

// NewClaudeCodexInterceptor creates a new interceptor
func NewClaudeCodexInterceptor(codexHandler, claudeHandler gin.HandlerFunc, enabled bool) *ClaudeCodexInterceptor {
	return &ClaudeCodexInterceptor{
		codexHandler:  codexHandler,
		claudeHandler: claudeHandler,
		enabled:       enabled,
	}
}

// Handler returns the gin handler function
func (cci *ClaudeCodexInterceptor) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cci.enabled {
			cci.claudeHandler(c)
			return
		}

		// Read request body
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Errorf("claude-codex interceptor: failed to read body: %v", err)
			cci.claudeHandler(c)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Extract model
		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			log.Errorf("claude-codex interceptor: failed to parse request: %v", err)
			cci.claudeHandler(c)
			return
		}

		// Check if model should be intercepted
		if !claude_to_codex.ShouldIntercept(req.Model) {
			log.Debugf("claude-codex interceptor: model %s not in intercept list, using Claude", req.Model)
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			cci.claudeHandler(c)
			return
		}

		log.Infof("claude-codex interceptor: intercepting %s → %s", req.Model, claude_to_codex.GetCodexModel(req.Model))

		// Translate request
		var claudeReq claude_to_codex.ClaudeRequest
		if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
			log.Errorf("claude-codex interceptor: failed to parse Claude request: %v", err)
			cci.claudeHandler(c)
			return
		}

		openaiReq, err := claude_to_codex.TranslateRequest(&claudeReq)
		if err != nil {
			log.Errorf("claude-codex interceptor: translation failed: %v", err)
			cci.claudeHandler(c)
			return
		}

		// Store original model for response translation
		c.Set("original_claude_model", claudeReq.Model)
		c.Set("claude_to_codex_intercept", true)

		// Replace request body with translated request
		translatedBody, _ := json.Marshal(openaiReq)
		c.Request.Body = io.NopCloser(bytes.NewReader(translatedBody))
		c.Request.ContentLength = int64(len(translatedBody))

		// Modify path to OpenAI endpoint
		c.Request.URL.Path = "/v1/chat/completions"

		// Use Codex handler
		cci.codexHandler(c)
	}
}
```

### Bước 4: Cấu hình Model Routing

Sửa file: `internal/api/modules/amp/routes.go`

Thêm logic intercept vào route `/api/provider/anthropic/v1/messages`:

```go
// In RegisterRoutes function, modify the anthropic route:

// Check if claude-to-codex intercept is enabled
interceptEnabled := cfg.ClaudeToCodexIntercept // Add this config option

if interceptEnabled {
    interceptor := handlers.NewClaudeCodexInterceptor(
        s.OpenAIHandler(),  // Codex uses OpenAI format
        s.ClaudeHandler(),
        true,
    )
    group.POST("/api/provider/anthropic/v1/messages", interceptor.Handler())
} else {
    group.POST("/api/provider/anthropic/v1/messages", s.ClaudeHandler())
}
```

---

## Cấu hình

### Thêm config option

Sửa file `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    
    // ClaudeToCodexIntercept enables routing Claude requests to Codex (GPT-5.1)
    ClaudeToCodexIntercept bool `yaml:"claude-to-codex-intercept" json:"claude-to-codex-intercept"`
    
    // ClaudeToCodexModels specifies which Claude models to intercept
    // If empty, uses default mapping (opus → gpt-5.1, sonnet → gpt-5.1-mini, haiku → gpt-5-turbo)
    ClaudeToCodexModels map[string]string `yaml:"claude-to-codex-models" json:"claude-to-codex-models"`
}
```

### Ví dụ config.yaml

#### Cấu hình TẮT (Mặc định - Luồng gốc 100%)

```yaml
# ============================================================
# LUỒNG GỐC: Amp CLI → Claude Handler → Azure Claude
# ============================================================

port: 8317
auth-dir: "~/.cli-proxy-api"
amp-upstream-url: "https://ampcode.com"

# TẮT intercept - request đi thẳng đến Claude (DEFAULT)
claude-to-codex-intercept: false

# Claude config vẫn hoạt động bình thường
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    # ... other settings
```

#### Cấu hình BẬT (Chuyển hướng Claude → Codex)

```yaml
# ============================================================
# INTERCEPT: Amp CLI → Translator → Codex (GPT-5.1)
# ============================================================

port: 8317
auth-dir: "~/.cli-proxy-api"
amp-upstream-url: "https://ampcode.com"

# BẬT intercept - request Claude được chuyển sang Codex
claude-to-codex-intercept: true

# Custom model mappings (optional - có default sẵn)
claude-to-codex-models:
  claude-opus-4-5-20251101: "gpt-5.1"
  claude-opus-4-5: "gpt-5.1"
  claude-sonnet-4-5: "gpt-5.1-mini"
  claude-haiku-4-5: "gpt-5-turbo"

# QUAN TRỌNG: Cần có Codex OAuth token
# Chạy: ./cli-proxy-api --codex-login
# Token sẽ được lưu tại: ~/.cli-proxy-api/codex-*.json

# Claude config vẫn có thể giữ (dùng cho model không trong mapping)
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
```

#### Chuyển đổi runtime (Hot-reload)

CLIProxyAPI hỗ trợ hot-reload config. Để chuyển đổi mà không cần restart:

```bash
# Sửa config.yaml: claude-to-codex-intercept: true/false

# CLIProxyAPI tự động detect thay đổi và reload
# Hoặc trigger manual reload qua Management API:
curl -X POST http://localhost:8317/v0/management/reload
```

---

## Testing

### 1. Unit Tests

Tạo file `internal/translator/claude_to_codex/translator_test.go`:

```go
package claude_to_codex

import (
	"encoding/json"
	"testing"
)

func TestTranslateRequest_SimpleText(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model: "claude-opus-4-5",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "Hello, world!"},
		},
		MaxTokens: 1000,
	}

	openaiReq, err := TranslateRequest(claudeReq)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}

	if openaiReq.Model != "gpt-5.1" {
		t.Errorf("Expected model gpt-5.1, got %s", openaiReq.Model)
	}

	if len(openaiReq.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(openaiReq.Messages))
	}

	if openaiReq.Messages[0].Role != "user" {
		t.Errorf("Expected role user, got %s", openaiReq.Messages[0].Role)
	}
}

func TestTranslateRequest_WithSystem(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model:  "claude-opus-4-5",
		System: "You are a helpful assistant.",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	openaiReq, err := TranslateRequest(claudeReq)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}

	if len(openaiReq.Messages) != 2 {
		t.Errorf("Expected 2 messages (system + user), got %d", len(openaiReq.Messages))
	}

	if openaiReq.Messages[0].Role != "system" {
		t.Errorf("Expected first message role system, got %s", openaiReq.Messages[0].Role)
	}
}

func TestTranslateRequest_WithTools(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model: "claude-opus-4-5",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get current weather",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]string{"type": "string"},
					},
				},
			},
		},
	}

	openaiReq, err := TranslateRequest(claudeReq)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}

	if len(openaiReq.Tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(openaiReq.Tools))
	}

	if openaiReq.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Expected tool name get_weather, got %s", openaiReq.Tools[0].Function.Name)
	}
}

func TestTranslateResponse_Simple(t *testing.T) {
	openaiResp := &OpenAIResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-5.1",
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you?",
				},
				FinishReason: "stop",
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	claudeResp := TranslateResponse(openaiResp, "claude-opus-4-5")

	if claudeResp.Type != "message" {
		t.Errorf("Expected type message, got %s", claudeResp.Type)
	}

	if claudeResp.Model != "claude-opus-4-5" {
		t.Errorf("Expected model claude-opus-4-5, got %s", claudeResp.Model)
	}

	if len(claudeResp.Content) != 1 {
		t.Errorf("Expected 1 content block, got %d", len(claudeResp.Content))
	}

	if claudeResp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason end_turn, got %s", claudeResp.StopReason)
	}
}
```

### 2. Integration Test

```bash
# Start CLIProxyAPI with intercept enabled
./cli-proxy-api --config config-intercept.yaml

# Test Claude endpoint (should be intercepted to Codex)
curl -X POST http://localhost:8317/api/provider/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-opus-4-5",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello, what model are you?"}]
  }'

# Expected: Response should mention GPT-5.1 internally but formatted as Claude response
```

### 3. End-to-End Test với Amp CLI

```bash
# Configure Amp to use proxy
export AMP_URL=http://localhost:8317

# Use Amp CLI
amp "What model are you using right now?"

# Check logs
tail -f run.log | grep -E "(intercept|codex|gpt-5)"
```

---

## Troubleshooting

### Common Issues

| Vấn đề | Nguyên nhân | Giải pháp |
|--------|-------------|-----------|
| Request không được intercept | Model không trong mapping | Thêm model vào `ModelMapping` |
| Response format sai | Translator lỗi | Check log, enable debug mode |
| Streaming bị đứt | SSE format không đúng | Verify `formatSSE` function |
| Tool calls không hoạt động | Tool format khác nhau | Debug `translateTools` function |
| 401/403 từ Codex | OAuth token hết hạn | Re-login: `./cli-proxy-api --codex-login` |

### Debug Mode

```yaml
# config.yaml
debug: true
logging-to-file: true
```

```bash
# View detailed logs
tail -f run.log | grep -E "(claude-codex|intercept|translate)"
```

### Verify Translation

```bash
# Enable request logging
curl -X PUT http://localhost:8317/v0/management/request-log \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'

# Make a test request
curl -X POST http://localhost:8317/api/provider/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-opus-4-5", "messages": [{"role": "user", "content": "Hi"}]}'

# Check logged request to verify translation
curl http://localhost:8317/v0/management/request-log | jq
```

---

## Tham khảo

### API Documentation

- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)

### CLIProxyAPI Internal Docs

- [SDK Advanced: Executors & Translators](sdk-advanced.md)
- [Amp CLI Integration](amp-cli-integration.md)

### Related Projects

- [claude-code-provider-proxy](https://github.com/ujisati/claude-code-provider-proxy) - Reference implementation
- [claude-code-proxy](https://github.com/fuergaosi233/claude-code-proxy) - Similar approach

---

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2025-11-27 | Initial documentation |

---

## Disclaimer

Tài liệu này cung cấp hướng dẫn kỹ thuật để tùy chỉnh CLIProxyAPI. Việc sử dụng các API theo cách không được chính thức hỗ trợ có thể vi phạm Terms of Service của các nhà cung cấp. Người dùng tự chịu trách nhiệm về việc sử dụng.
