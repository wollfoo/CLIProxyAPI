# Cross-Provider Routing Implementation

## Tổng quan

Tính năng **Cross-Provider Routing** cho phép CLIProxyAPI redirect requests từ một provider (ví dụ: OpenAI GPT-5) sang provider khác (ví dụ: Azure Claude Opus 4.5) với protocol translation tự động.

### Use Case chính
- **Amp CLI Oracle subagent** sử dụng GPT-5.1 (OpenAI)
- Redirect sang **Azure AI Foundry Claude Opus 4.5** (Anthropic)
- Request được translate: OpenAI Chat Completions → Claude Messages API
- Response được translate ngược: Claude → OpenAI format

---

## Các thay đổi code

### 1. Config struct - `internal/config/config.go`

Thêm fields mới vào `CodexKey` struct:

```go
type CodexKey struct {
    APIKey      string            `yaml:"api-key"`
    BaseURL     string            `yaml:"base-url"`
    Headers     map[string]string `yaml:"headers"`
    ProxyURL    string            `yaml:"proxy-url"`
    ProviderType string           `yaml:"provider-type"` // NEW: "claude", "gemini", etc.
    Models      []CodexModel      `yaml:"models"`        // NEW: Model alias mappings
}

type CodexModel struct {
    Name  string `yaml:"name"`   // Target model (e.g., "claude-opus-4-5")
    Alias string `yaml:"alias"`  // Source alias (e.g., "gpt-5")
}
```

### 2. CrossProviderExecutor - `internal/runtime/executor/cross_provider_executor.go`

Executor mới xử lý cross-provider routing:

```go
type CrossProviderExecutor struct {
    cfg          *config.Config
    providerType string // Target provider (e.g., "claude")
}

func NewCrossProviderExecutor(cfg *config.Config, providerType string) *CrossProviderExecutor
```

**Chức năng chính:**
- `Execute()` - Non-streaming requests
- `ExecuteStream()` - Streaming requests  
- `CountTokens()` - Token counting
- `resolveUpstreamModel()` - Resolve model alias từ auth attributes
- `extractSystemToTopLevel()` - Fix system message format cho Claude API

### 3. Auth synthesis - `internal/watcher/watcher.go`

Tạo cross-provider auth entries từ config:

```go
// synthesizeAuths() - thêm logic cho cross-provider
if providerType != "" {
    for _, model := range ck.Models {
        attrs := map[string]string{
            "api_key":       key,
            "provider_type": providerType,
            "model_alias":   model.Alias,
            "model_name":    model.Name,    // Target model
            "base_url":      ck.BaseURL,
        }
        auth := &coreauth.Auth{
            Provider: "cross-provider-" + providerType,
            // ...
        }
    }
}
```

### 4. Executor registration - `sdk/cliproxy/service.go`

Đăng ký CrossProviderExecutor cho providers bắt đầu với "cross-provider-":

```go
// ensureExecutorsForAuth()
if strings.HasPrefix(provider, "cross-provider-") {
    targetProvider := strings.TrimPrefix(providerKey, "cross-provider-")
    s.coreManager.RegisterExecutor(executor.NewCrossProviderExecutor(s.cfg, targetProvider))
    return
}
```

### 5. Model registration - `sdk/cliproxy/service.go`

Đăng ký cross-provider models vào registry:

```go
// registerModelsForAuth()
if strings.HasPrefix(provider, "cross-provider-") {
    alias := auth.Attributes["model_alias"]
    modelName := auth.Attributes["model_name"]
    models = []*ModelInfo{{
        ID:          alias,
        DisplayName: fmt.Sprintf("%s → %s", alias, modelName),
        // ...
    }}
    GlobalModelRegistry().RegisterClient(a.ID, provider, models)
}
```

### 6. Fallback handlers - `internal/api/modules/amp/fallback_handlers.go`

Thêm detection cho cross-provider aliases:

```go
func hasCodexCrossProviderAlias(cfg *config.Config, model string) bool
func GetCodexCrossProviderConfig(cfg *config.Config, model string) *config.CodexKey
```

### 7. Provider utilities - `internal/util/provider.go`

Helper functions:

```go
func IsCrossProviderAlias(cfg *config.Config, model string) bool
func GetCrossProviderConfig(cfg *config.Config, model string) *config.CodexKey
```

---

## Các bug đã fix

### Bug 1: Executor sai được sử dụng

**Triệu chứng:** Request gpt-5.1 dùng `OpenAICompatExecutor` thay vì `CrossProviderExecutor`

**Nguyên nhân:** Thiếu case cho "cross-provider-*" trong `ensureExecutorsForAuth()`

**Fix:** Thêm check `strings.HasPrefix(provider, "cross-provider-")`

### Bug 2: Model không được resolve

**Triệu chứng:** `DeploymentNotFound: gpt-5.1 does not exist`

**Nguyên nhân:** `resolveUpstreamModel()` tìm `provider_key` attribute nhưng auth chỉ có `provider_type`

**Fix:** Đơn giản hóa - lấy `model_name` trực tiếp từ auth attributes

### Bug 3: System role error

**Triệu chứng:** 
```json
{"error":"messages: Unexpected role \"system\". The Messages API accepts a top-level `system` parameter"}
```

**Nguyên nhân:** Claude API cần `system` ở top-level parameter, không phải trong messages array

**Fix:** Thêm `extractSystemToTopLevel()` function để:
1. Tìm messages có role "system"
2. Extract content ra top-level `system` parameter
3. Remove system messages khỏi messages array

---

## Config example

File: `config.oracle-to-azure-claude.yaml`

```yaml
# Cross-provider routing: Oracle GPT-5 → Azure Claude
codex-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    provider-type: "claude"
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "YOUR_AZURE_API_KEY"
    models:
      - name: "claude-opus-4-5"
        alias: "gpt-5"
      - name: "claude-opus-4-5"
        alias: "gpt-5.1"
      - name: "claude-opus-4-5"
        alias: "gpt-5-minimal"
      - name: "claude-opus-4-5"
        alias: "gpt-5-low"
      - name: "claude-opus-4-5"
        alias: "gpt-5-medium"
      - name: "claude-opus-4-5"
        alias: "gpt-5-high"

# Claude API key cho các Claude models khác
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    headers:
      anthropic-version: "2023-06-01"
    models:
      # Redirect tất cả subagents → Opus
      - name: "claude-opus-4-5"
        alias: "claude-sonnet-4-5"      # Librarian
      - name: "claude-opus-4-5"
        alias: "claude-haiku-4-5"       # Search
      - name: "claude-opus-4-5"
        alias: "claude-haiku-3-5"       # Titling
```

---

## Amp CLI Models mapping

| Component | Model mặc định | Redirect → |
|-----------|---------------|------------|
| **Smart mode** | Claude Opus 4.5 | claude-opus-4-5 |
| **Rush mode** | Claude Haiku 4.5 | claude-opus-4-5 |
| **Search** | Claude Haiku 4.5 | claude-opus-4-5 |
| **Oracle** | GPT-5.1 | claude-opus-4-5 (cross-provider) |
| **Librarian** | Claude Sonnet 4.5 | claude-opus-4-5 |
| **Titling** | Claude Haiku 3.5 | claude-opus-4-5 |
| **Handoff** | Gemini 2.5 Flash | (không redirect) |
| **Topics** | Gemini 2.5 Flash-Lite | (không redirect) |

---

## Request flow

```
Amp CLI (Oracle)
    │
    ▼ Request: model="gpt-5.1"
┌─────────────────────────────────────┐
│         CLIProxyAPI                 │
│                                     │
│  1. Manager selects auth            │
│     cross-provider:claude:gpt-5.1   │
│                                     │
│  2. CrossProviderExecutor           │
│     - Translate OpenAI → Claude     │
│     - resolveUpstreamModel()        │
│       gpt-5.1 → claude-opus-4-5     │
│     - extractSystemToTopLevel()     │
│     - sanitizeToolNames()           │
│                                     │
│  3. HTTP POST to Azure Claude       │
│     model: "claude-opus-4-5"        │
│                                     │
│  4. Response: Claude → OpenAI       │
│     translation                     │
└─────────────────────────────────────┘
    │
    ▼ Response: OpenAI format
Amp CLI (Oracle)
```

---

## Files modified

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add ProviderType, Models to CodexKey |
| `internal/runtime/executor/cross_provider_executor.go` | NEW: CrossProviderExecutor |
| `internal/watcher/watcher.go` | Add cross-provider auth synthesis |
| `sdk/cliproxy/service.go` | Register executor & models |
| `internal/api/modules/amp/fallback_handlers.go` | Add cross-provider detection |
| `internal/util/provider.go` | Add helper functions |
| `internal/runtime/executor/claude_executor.go` | Add debug logging |

---

## Testing

1. Build binary:
```bash
go build -o cli-proxy-api ./cmd/server
```

2. Run với config:
```bash
./cli-proxy-api --config ./config.oracle-to-azure-claude.yaml
```

3. Verify models registered:
```bash
curl -s http://localhost:8317/v1/models | jq '.data[].id'
```

Expected output includes: `gpt-5`, `gpt-5.1`, `claude-opus-4-5`, etc.

4. Test Oracle trong Amp CLI và kiểm tra logs:
```
[debug] cross-provider executor: model alias gpt-5.1 → claude-opus-4-5
[debug] cross-provider executor: extracted 1 system parts to top-level
```

---

## Date: 2025-11-30
## Branch: feature/azure-claude-provider
