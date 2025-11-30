# Kết quả rà soát: CLIProxyAPI đã có sẵn Protocol Translator

> **Tài liệu**: Phân tích hệ thống Protocol Translator trong CLIProxyAPI  
> **Ngày tạo**: 2025-11-30  
> **Phiên bản**: 1.0  
> **Mục đích**: Đánh giá khả năng tích hợp Oracle GPT-5 → Claude redirection

---

## Mục lục

- [Phát hiện quan trọng](#phát-hiện-quan-trọng)
- [Kiến trúc Translator System](#kiến-trúc-translator-system)
- [Chi tiết OpenAI → Claude Translator](#chi-tiết-openai--claude-translator)
- [Đề xuất tích hợp](#đề-xuất-tích-hợp)
- [Thay đổi code cần thiết](#thay-đổi-code-cần-thiết)
- [Luồng hoạt động](#luồng-hoạt-động)
- [Tóm tắt](#tóm-tắt)

---

## Phát hiện quan trọng

**CLIProxyAPI đã có sẵn hệ thống Protocol Translator hoàn chỉnh** với **28 translator modules**!

### Danh sách Translator Modules

```
internal/translator/
├── antigravity/
│   ├── claude/init.go
│   ├── gemini/init.go
│   └── openai/
│       ├── chat-completions/init.go
│       └── responses/init.go
│
├── claude/
│   ├── gemini/init.go
│   ├── gemini-cli/init.go
│   └── openai/
│       ├── chat-completions/init.go    ← OpenAI → Claude (QUAN TRỌNG!)
│       └── responses/init.go
│
├── codex/
│   ├── claude/init.go                   ← Codex → Claude
│   ├── gemini/init.go
│   ├── gemini-cli/init.go
│   └── openai/
│       ├── chat-completions/init.go
│       └── responses/init.go
│
├── gemini/
│   ├── claude/init.go
│   ├── gemini/init.go
│   ├── gemini-cli/init.go
│   └── openai/
│       ├── chat-completions/init.go
│       └── responses/init.go
│
├── gemini-cli/
│   ├── claude/init.go
│   ├── gemini/init.go
│   └── openai/
│       ├── chat-completions/init.go
│       └── responses/init.go
│
├── openai/
│   ├── claude/init.go                   ← Claude → OpenAI
│   ├── gemini/init.go
│   ├── gemini-cli/init.go
│   └── openai/
│       ├── chat-completions/init.go
│       └── responses/init.go
│
└── init.go
```

---

## Kiến trúc Translator System

### Registry Pattern

CLIProxyAPI sử dụng Registry pattern để quản lý translators:

```go
// sdk/translator/registry.go
type Registry struct {
    mu        sync.RWMutex
    requests  map[Format]map[Format]RequestTransform   // Request translators
    responses map[Format]map[Format]ResponseTransform  // Response translators
}

// Register stores request/response transforms between two formats
func (r *Registry) Register(from, to Format, request RequestTransform, response ResponseTransform)

// TranslateRequest converts a payload between schemas
func (r *Registry) TranslateRequest(from, to Format, model string, rawJSON []byte, stream bool) []byte

// TranslateStream applies the registered streaming response translator
func (r *Registry) TranslateStream(ctx context.Context, from, to Format, ...) []string

// TranslateNonStream applies the registered non-stream response translator
func (r *Registry) TranslateNonStream(ctx context.Context, from, to Format, ...) string
```

### Translator Interface

```go
// Request translator function signature
type TranslateRequestFunc func(modelName string, rawJSON []byte, stream bool) []byte

// Response translator interface
type TranslateResponse struct {
    Stream     func(ctx context.Context, model string, ...) []string
    NonStream  func(ctx context.Context, model string, ...) string
    TokenCount func(ctx context.Context, count int64) string
}
```

### Cách Translator được đăng ký

Mỗi translator module tự đăng ký trong hàm `init()`:

```go
// internal/translator/claude/openai/chat-completions/init.go
func init() {
    translator.Register(
        OpenAI,                              // FROM format
        Claude,                              // TO format
        ConvertOpenAIRequestToClaude,        // Request transformer
        interfaces.TranslateResponse{
            Stream:    ConvertClaudeResponseToOpenAI,
            NonStream: ConvertClaudeResponseToOpenAINonStream,
        },
    )
}
```

---

## Chi tiết OpenAI → Claude Translator

### Vị trí file

```
internal/translator/claude/openai/chat-completions/
├── init.go                          # Đăng ký translator
├── claude_openai_request.go         # OpenAI → Claude request
└── claude_openai_response.go        # Claude → OpenAI response
```

### Request Translation: OpenAI → Claude

**File**: `claude_openai_request.go`

```go
// ConvertOpenAIRequestToClaude parses and transforms an OpenAI Chat Completions 
// API request into Claude Code API format.
func ConvertOpenAIRequestToClaude(modelName string, inputRawJSON []byte, stream bool) []byte
```

**Chức năng chính:**

| Feature | OpenAI Format | Claude Format |
|---------|---------------|---------------|
| **Messages** | `messages[].role/content` | `messages[].role/content` với format khác |
| **System prompt** | `messages[0].role="system"` | `system` field riêng |
| **Max tokens** | `max_tokens` | `max_tokens` |
| **Temperature** | `temperature` | `temperature` |
| **Top P** | `top_p` | `top_p` |
| **Stop sequences** | `stop` | `stop_sequences` |
| **Tool calls** | `tool_calls[].function` | `content[].type="tool_use"` |
| **Tool results** | `messages[].role="tool"` | `content[].type="tool_result"` |
| **Images** | `content[].image_url.url` | `content[].source.type="base64"` |

**Code example:**

```go
// Input OpenAI format
{
    "model": "gpt-5",
    "messages": [
        {"role": "system", "content": "You are helpful"},
        {"role": "user", "content": "Hello"}
    ],
    "max_tokens": 1000
}

// Output Claude format
{
    "model": "claude-opus-4-5",
    "system": "You are helpful",
    "messages": [
        {"role": "user", "content": [{"type": "text", "text": "Hello"}]}
    ],
    "max_tokens": 1000
}
```

### Response Translation: Claude → OpenAI

**File**: `claude_openai_response.go`

```go
// ConvertClaudeResponseToOpenAI converts Claude Code streaming response 
// to OpenAI Chat Completions format (streaming)
func ConvertClaudeResponseToOpenAI(ctx context.Context, modelName string, ...) []string

// ConvertClaudeResponseToOpenAINonStream converts non-streaming response
func ConvertClaudeResponseToOpenAINonStream(ctx context.Context, ...) string
```

**Mapping stop reasons:**

| Claude | OpenAI |
|--------|--------|
| `end_turn` | `stop` |
| `tool_use` | `tool_calls` |
| `max_tokens` | `length` |
| `stop_sequence` | `stop` |

---

## Đề xuất tích hợp

### Phương án: Cross-Provider Model Routing

Thêm tính năng mới vào `codex-api-key` để sử dụng Claude executor với built-in translation.

### Config đề xuất (config.yaml)

```yaml
# Redirect Oracle GPT-5 → Claude Opus on Azure AI Foundry
codex-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    
    # NEW: Cross-provider routing với built-in translation
    provider-type: "claude"  # Sử dụng Claude executor + translator
    
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "YOUR_AZURE_API_KEY"
    
    models:
      # Map GPT-5 aliases → Claude models
      - name: "claude-opus-4-5"
        alias: "gpt-5"
      - name: "claude-opus-4-5"
        alias: "gpt-5.1"
      - name: "claude-sonnet-4-5"
        alias: "gpt-5-mini"
```

---

## Thay đổi code cần thiết

### 1. Cập nhật Config struct

```go
// internal/config/config.go

// CodexKey represents the configuration for a Codex API key
type CodexKey struct {
    // Existing fields
    APIKey   string            `yaml:"api-key" json:"api-key"`
    BaseURL  string            `yaml:"base-url" json:"base-url"`
    ProxyURL string            `yaml:"proxy-url" json:"proxy-url"`
    Headers  map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
    
    // NEW: Provider type for cross-provider routing
    // Options: "openai" (default), "claude", "gemini"
    ProviderType string `yaml:"provider-type,omitempty" json:"provider-type,omitempty"`
    
    // NEW: Models with alias support (similar to claude-api-key)
    Models []CodexModel `yaml:"models,omitempty" json:"models,omitempty"`
}

// CodexModel describes a mapping between an alias and the actual upstream model name
type CodexModel struct {
    // Name is the upstream model identifier used when issuing requests
    Name string `yaml:"name" json:"name"`
    
    // Alias is the client-facing model name that maps to Name
    Alias string `yaml:"alias" json:"alias"`
}
```

### 2. Tạo Cross-Provider Executor

```go
// internal/runtime/executor/cross_provider_executor.go
package executor

import (
    "bytes"
    "context"
    
    "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
    cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
    cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
    sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

// CrossProviderExecutor handles requests that need protocol translation
// Example: OpenAI request → Claude backend
type CrossProviderExecutor struct {
    sourceFormat   string // e.g., "openai" (incoming request format)
    targetFormat   string // e.g., "claude" (backend API format)
    targetExecutor cliproxyexecutor.Executor
    cfg            *config.Config
}

// NewCrossProviderExecutor creates a new cross-provider executor
func NewCrossProviderExecutor(
    sourceFormat, targetFormat string,
    targetExecutor cliproxyexecutor.Executor,
    cfg *config.Config,
) *CrossProviderExecutor {
    return &CrossProviderExecutor{
        sourceFormat:   sourceFormat,
        targetFormat:   targetFormat,
        targetExecutor: targetExecutor,
        cfg:            cfg,
    }
}

// Identifier returns the executor identifier
func (e *CrossProviderExecutor) Identifier() string {
    return "cross-provider-" + e.sourceFormat + "-to-" + e.targetFormat
}

// Execute handles non-streaming requests with protocol translation
func (e *CrossProviderExecutor) Execute(
    ctx context.Context,
    auth *cliproxyauth.Auth,
    req cliproxyexecutor.Request,
    opts cliproxyexecutor.Options,
) (resp cliproxyexecutor.Response, err error) {
    // 1. Translate request: source format → target format
    from := sdktranslator.FromString(e.sourceFormat)
    to := sdktranslator.FromString(e.targetFormat)
    
    translatedPayload := sdktranslator.TranslateRequest(
        from, to, req.Model, bytes.Clone(req.Payload), opts.Stream,
    )
    
    // 2. Execute with target executor (e.g., Claude)
    translatedReq := req
    translatedReq.Payload = translatedPayload
    
    // Update opts to use target format
    translatedOpts := opts
    translatedOpts.SourceFormat = to
    
    resp, err = e.targetExecutor.Execute(ctx, auth, translatedReq, translatedOpts)
    if err != nil {
        return resp, err
    }
    
    // 3. Translate response: target format → source format
    var param any
    translatedResponse := sdktranslator.TranslateNonStream(
        ctx, to, from, req.Model,
        opts.OriginalRequest, translatedPayload, resp.Payload, &param,
    )
    
    resp.Payload = []byte(translatedResponse)
    return resp, nil
}

// ExecuteStream handles streaming requests with protocol translation
func (e *CrossProviderExecutor) ExecuteStream(
    ctx context.Context,
    auth *cliproxyauth.Auth,
    req cliproxyexecutor.Request,
    opts cliproxyexecutor.Options,
) (<-chan cliproxyexecutor.StreamChunk, error) {
    // 1. Translate request
    from := sdktranslator.FromString(e.sourceFormat)
    to := sdktranslator.FromString(e.targetFormat)
    
    translatedPayload := sdktranslator.TranslateRequest(
        from, to, req.Model, bytes.Clone(req.Payload), true,
    )
    
    // 2. Execute streaming with target executor
    translatedReq := req
    translatedReq.Payload = translatedPayload
    
    translatedOpts := opts
    translatedOpts.SourceFormat = to
    
    targetStream, err := e.targetExecutor.ExecuteStream(ctx, auth, translatedReq, translatedOpts)
    if err != nil {
        return nil, err
    }
    
    // 3. Create output channel for translated chunks
    out := make(chan cliproxyexecutor.StreamChunk)
    
    go func() {
        defer close(out)
        var param any
        
        for chunk := range targetStream {
            if chunk.Err != nil {
                out <- chunk
                continue
            }
            
            // Translate each streaming chunk
            translatedChunks := sdktranslator.TranslateStream(
                ctx, to, from, req.Model,
                opts.OriginalRequest, translatedPayload, chunk.Payload, &param,
            )
            
            for _, translated := range translatedChunks {
                out <- cliproxyexecutor.StreamChunk{Payload: []byte(translated)}
            }
        }
    }()
    
    return out, nil
}

// Refresh delegates to the target executor
func (e *CrossProviderExecutor) Refresh(
    ctx context.Context,
    auth *cliproxyauth.Auth,
) (*cliproxyauth.Auth, error) {
    return e.targetExecutor.Refresh(ctx, auth)
}

// PrepareRequest delegates to the target executor
func (e *CrossProviderExecutor) PrepareRequest(
    req *http.Request,
    auth *cliproxyauth.Auth,
) error {
    return e.targetExecutor.PrepareRequest(req, auth)
}
```

### 3. Cập nhật Fallback Handler

```go
// internal/api/modules/amp/fallback_handlers.go

// hasCodexWithClaudeProvider checks if model matches codex-api-key with provider-type: "claude"
func (fh *FallbackHandler) hasCodexWithClaudeProvider(modelName string) (*config.CodexKey, *config.CodexModel) {
    if fh.getConfig == nil {
        return nil, nil
    }
    cfg := fh.getConfig()
    if cfg == nil {
        return nil, nil
    }
    
    modelLower := strings.ToLower(strings.TrimSpace(modelName))
    
    for i := range cfg.CodexKey {
        ck := &cfg.CodexKey[i]
        
        // Check if this codex-api-key uses Claude provider
        if !strings.EqualFold(ck.ProviderType, "claude") {
            continue
        }
        
        // Check model aliases
        for j := range ck.Models {
            model := &ck.Models[j]
            if strings.EqualFold(strings.TrimSpace(model.Alias), modelLower) {
                return ck, model
            }
        }
    }
    return nil, nil
}
```

### 4. Cập nhật Provider Registration

```go
// internal/runtime/registry/provider_registry.go

// RegisterCodexProviders registers codex-api-key providers
func RegisterCodexProviders(cfg *config.Config) {
    for _, ck := range cfg.CodexKey {
        if ck.ProviderType == "claude" {
            // Create Claude executor wrapped with cross-provider translation
            claudeExecutor := executor.NewClaudeExecutor(cfg)
            crossProviderExecutor := executor.NewCrossProviderExecutor(
                "openai",  // Source: OpenAI format (from Oracle)
                "claude",  // Target: Claude format (backend)
                claudeExecutor,
                cfg,
            )
            
            // Register for each model alias
            for _, model := range ck.Models {
                registry.RegisterModel(model.Alias, crossProviderExecutor)
            }
        }
    }
}
```

---

## Luồng hoạt động

### Sequence Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    TÍCH HỢP BUILT-IN TRANSLATOR                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Amp CLI Oracle                                                             │
│       │                                                                     │
│       │ OpenAI Protocol: POST /api/provider/openai/v1/chat/completions      │
│       │ model: "gpt-5"                                                      │
│       ▼                                                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ CLIProxyAPI (port 8317)                                             │   │
│  │                                                                     │   │
│  │ 1. Nhận request OpenAI format                                       │   │
│  │ 2. Check codex-api-key với provider-type: "claude"                  │   │
│  │ 3. Match alias "gpt-5" → name "claude-opus-4-5"                     │   │
│  │                                                                     │   │
│  │ ┌─────────────────────────────────────────────────────────────────┐ │   │
│  │ │ CrossProviderExecutor                                           │ │   │
│  │ │                                                                 │ │   │
│  │ │ a. TranslateRequest(OpenAI → Claude)                            │ │   │
│  │ │    - messages[].role="system" → system field                    │ │   │
│  │ │    - tool_calls → tool_use                                      │ │   │
│  │ │    - stop → stop_sequences                                      │ │   │
│  │ │                                                                 │ │   │
│  │ │ b. Execute với ClaudeExecutor                                   │ │   │
│  │ │                                                                 │ │   │
│  │ │ c. TranslateResponse(Claude → OpenAI)                           │ │   │
│  │ │    - content[].text → choices[].message.content                 │ │   │
│  │ │    - tool_use → tool_calls                                      │ │   │
│  │ │    - end_turn → stop                                            │ │   │
│  │ └─────────────────────────────────────────────────────────────────┘ │   │
│  │                                                                     │   │
│  │ 4. Forward translated request tới Azure AI Foundry                  │   │
│  └──────────────────────────┬──────────────────────────────────────────┘   │
│                             │                                               │
│                             │ Claude Protocol: POST /v1/messages            │
│                             │ model: "claude-opus-4-5"                      │
│                             ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ Azure AI Foundry                                                    │   │
│  │ https://YOUR_RESOURCE.services.ai.azure.com/anthropic               │   │
│  │                                                                     │   │
│  │ Xử lý request và trả về Claude response                             │   │
│  └──────────────────────────┬──────────────────────────────────────────┘   │
│                             │                                               │
│                             │ Claude Response                               │
│                             ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ CLIProxyAPI - Response Translation                                  │   │
│  │                                                                     │   │
│  │ TranslateResponse(Claude → OpenAI)                                  │   │
│  │   - Convert content blocks → choices                                │   │
│  │   - Convert tool_use → tool_calls                                   │   │
│  │   - Map stop_reason → finish_reason                                 │   │
│  │   - Map usage tokens                                                │   │
│  └──────────────────────────┬──────────────────────────────────────────┘   │
│                             │                                               │
│                             │ OpenAI Response                               │
│                             ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ Amp CLI Oracle                                                      │   │
│  │                                                                     │   │
│  │ Nhận response OpenAI format                                         │   │
│  │ (Tưởng là từ GPT-5, thực tế là Claude Opus 4.5)                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Data Flow Example

**1. Original OpenAI Request (from Oracle):**
```json
{
    "model": "gpt-5",
    "messages": [
        {"role": "system", "content": "You are a code reviewer"},
        {"role": "user", "content": "Review this code: function add(a,b){return a+b}"}
    ],
    "max_tokens": 2000,
    "temperature": 0.7
}
```

**2. Translated Claude Request:**
```json
{
    "model": "claude-opus-4-5",
    "system": "You are a code reviewer",
    "messages": [
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "Review this code: function add(a,b){return a+b}"}
            ]
        }
    ],
    "max_tokens": 2000,
    "temperature": 0.7
}
```

**3. Claude Response:**
```json
{
    "id": "msg_xxx",
    "type": "message",
    "role": "assistant",
    "content": [
        {"type": "text", "text": "The function looks good..."}
    ],
    "stop_reason": "end_turn",
    "usage": {"input_tokens": 50, "output_tokens": 100}
}
```

**4. Translated OpenAI Response (to Oracle):**
```json
{
    "id": "chatcmpl-xxx",
    "object": "chat.completion",
    "model": "gpt-5",
    "choices": [{
        "index": 0,
        "message": {
            "role": "assistant",
            "content": "The function looks good..."
        },
        "finish_reason": "stop"
    }],
    "usage": {
        "prompt_tokens": 50,
        "completion_tokens": 100,
        "total_tokens": 150
    }
}
```

---

## Tóm tắt

### Kết quả rà soát

| Câu hỏi | Trả lời |
|---------|---------|
| **Có sẵn Protocol Translator không?** | ✅ **CÓ** - 28 translator modules |
| **Có OpenAI → Claude Translator không?** | ✅ **CÓ** - `internal/translator/claude/openai/` |
| **Đang được sử dụng ở đâu?** | Claude Executor, Gemini Executor, OpenAI Compat Executor |
| **Có thể tích hợp cho Oracle → Azure Claude không?** | ✅ **CÓ THỂ** - Cần thêm feature vào `codex-api-key` |

### Ưu điểm khi tích hợp built-in translator

| Ưu điểm | Mô tả |
|---------|-------|
| **Không cần external proxy** | Tất cả xử lý trong CLIProxyAPI |
| **Tận dụng code có sẵn** | Translator đã được test và production-ready |
| **Cấu hình đơn giản** | Chỉ cần thêm `provider-type: "claude"` vào config |
| **Hiệu năng tốt hơn** | Không có network hop bổ sung |
| **Bảo trì dễ dàng** | Một codebase duy nhất |

### Công việc cần thực hiện

1. **Config changes**: Thêm `ProviderType` và `Models` vào `CodexKey` struct
2. **New executor**: Tạo `CrossProviderExecutor` sử dụng `sdktranslator`
3. **Route handler**: Cập nhật fallback handler để detect cross-provider routing
4. **Provider registry**: Đăng ký cross-provider models
5. **Testing**: Test với Oracle → Azure Claude use case

### Files cần sửa đổi

```
internal/config/config.go                           # Add ProviderType, Models to CodexKey
internal/runtime/executor/cross_provider_executor.go # New executor (create)
internal/api/modules/amp/fallback_handlers.go       # Add cross-provider detection
internal/runtime/registry/provider_registry.go      # Register cross-provider models
```

---

## Tài liệu tham khảo

### Source files quan trọng

- `sdk/translator/registry.go` - Translator registry
- `internal/translator/claude/openai/chat-completions/` - OpenAI → Claude translator
- `internal/translator/openai/claude/` - Claude → OpenAI translator
- `internal/runtime/executor/claude_executor.go` - Claude executor reference

### External documentation

- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)
- [Azure AI Foundry Claude](https://learn.microsoft.com/en-us/azure/ai-services/openai/concepts/models)
