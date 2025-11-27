# Azure Claude Integration for Amp CLI

## Tài liệu kỹ thuật: Fork CLIProxyAPI để ép Amp CLI dùng Azure Claude

**Phiên bản**: 1.0  
**Ngày tạo**: 2025-11-27  
**Tác giả**: Azure Claude Integration Team

---

## Mục lục

1. [Tổng quan](#1-tổng-quan)
2. [Vấn đề cần giải quyết](#2-vấn-đề-cần-giải-quyết)
3. [Kiến trúc giải pháp](#3-kiến-trúc-giải-pháp)
4. [Chi tiết các file đã sửa đổi](#4-chi-tiết-các-file-đã-sửa-đổi)
5. [Cấu hình config.yaml](#5-cấu-hình-configyaml)
6. [Hướng dẫn build và triển khai](#6-hướng-dẫn-build-và-triển-khai)
7. [Kiểm thử](#7-kiểm-thử)
8. [Troubleshooting](#8-troubleshooting)

---

## 1. Tổng quan

### 1.1 Mục đích

Tài liệu này mô tả chi tiết cách sửa đổi mã nguồn CLIProxyAPI để **ép Amp CLI sử dụng Azure Foundry Anthropic endpoint** thay vì fallback về `ampcode.com` khi không có OAuth token.

### 1.2 Tóm tắt giải pháp

- **Trước khi sửa**: Amp CLI gọi Claude models → CLIProxyAPI check OAuth → Không có → Fallback sang `ampcode.com`
- **Sau khi sửa**: Amp CLI gọi Claude models → CLIProxyAPI check OAuth → Không có → **Check `claude-api-key` aliases** → Có match → Route đến Azure Foundry

### 1.3 Các thành phần liên quan

| Thành phần | Vai trò |
|------------|---------|
| `fallback_handlers.go` | Logic fallback chính, thêm check `claude-api-key` aliases |
| `amp.go` | Module Amp, lưu config reference |
| `routes.go` | Đăng ký routes, truyền config vào FallbackHandler |
| `config.yaml` | Cấu hình Azure Claude API key và model aliases |

---

## 2. Vấn đề cần giải quyết

### 2.1 Flow gốc của CLIProxyAPI

```
Amp CLI Request
    ↓
CLIProxyAPI nhận request tại /api/provider/anthropic/v1/messages
    ↓
FallbackHandler.WrapHandler() kiểm tra:
    - Trích xuất model name từ request body
    - Gọi util.GetProviderName(model) để tìm providers
    ↓
Nếu len(providers) == 0:
    → Fallback sang ampcode.com (proxy.ServeHTTP)
    ↓
Nếu có providers:
    → Gọi handler local (claudeCodeHandlers.ClaudeMessages)
```

### 2.2 Vấn đề

- `util.GetProviderName()` chỉ check model registry (populated từ OAuth auth entries)
- `claude-api-key` trong config.yaml **không được đăng ký vào registry**
- Kết quả: Amp CLI requests luôn fallback về `ampcode.com`

### 2.3 Yêu cầu

1. Khi model name match với aliases trong `claude-api-key`, **không fallback**
2. Route request đến `ClaudeExecutor` với Azure Foundry endpoint
3. Map model names từ Amp CLI sang Azure Foundry model names

---

## 3. Kiến trúc giải pháp

### 3.1 Flow sau khi sửa

```
Amp CLI Request (model: "claude-opus-4-5-20251101")
    ↓
CLIProxyAPI nhận request tại /api/provider/anthropic/v1/messages
    ↓
FallbackHandler.WrapHandler() kiểm tra:
    1. Trích xuất model name: "claude-opus-4-5-20251101"
    2. Gọi util.GetProviderName(model) → []  (empty)
    3. [MỚI] Gọi fh.hasClaudeAPIKeyAlias(model) → true
    4. Set providers = []string{"claude"}
    ↓
len(providers) > 0 → Gọi handler local
    ↓
claudeCodeHandlers.ClaudeMessages()
    ↓
ClaudeExecutor.Execute() với:
    - base-url: https://foundry-sonnet.services.ai.azure.com/anthropic
    - api-key: Azure Foundry API key
    - headers: anthropic-version, x-api-key
    - model: "claude-opus-4-5" (mapped từ alias)
    ↓
Azure Foundry Response
```

### 3.2 Diagram

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Amp CLI   │────▶│   CLIProxyAPI    │────▶│  Azure Foundry  │
│             │     │                  │     │                 │
│ model:      │     │ 1. Check OAuth   │     │ claude-opus-4-5 │
│ claude-opus │     │ 2. Check Aliases │     │ claude-sonnet   │
│ -4-5-20251  │     │ 3. Route Azure   │     │ claude-haiku    │
└─────────────┘     └──────────────────┘     └─────────────────┘
                            │
                            │ (Không fallback)
                            ▼
                    ┌──────────────────┐
                    │   ampcode.com    │
                    │   (Bypassed)     │
                    └──────────────────┘
```

---

## 4. Chi tiết các file đã sửa đổi

### 4.1 `internal/api/modules/amp/fallback_handlers.go`

#### 4.1.1 Thêm import

```go
import (
    "bytes"
    "encoding/json"
    "io"
    "net/http/httputil"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/router-for-me/CLIProxyAPI/v6/internal/config"  // [MỚI]
    "github.com/router-for-me/CLIProxyAPI/v6/internal/util"
    log "github.com/sirupsen/logrus"
)
```

#### 4.1.2 Sửa struct FallbackHandler

```go
// FallbackHandler wraps a standard handler with fallback logic to ampcode.com
// when the model's provider is not available in CLIProxyAPI
type FallbackHandler struct {
    getProxy  func() *httputil.ReverseProxy
    getConfig func() *config.Config  // [MỚI] Để check claude-api-key aliases
}
```

#### 4.1.3 Thêm constructor mới

```go
// NewFallbackHandlerWithConfig creates a new fallback handler wrapper with config access
// Cho phép check claude-api-key aliases để ép dùng Azure Claude thay vì fallback
func NewFallbackHandlerWithConfig(getProxy func() *httputil.ReverseProxy, getConfig func() *config.Config) *FallbackHandler {
    return &FallbackHandler{
        getProxy:  getProxy,
        getConfig: getConfig,
    }
}
```

#### 4.1.4 Sửa logic trong WrapHandler

```go
func (fh *FallbackHandler) WrapHandler(handler gin.HandlerFunc) gin.HandlerFunc {
    return func(c *gin.Context) {
        // ... existing code to read body and extract model ...

        // Check if we have providers for this model
        providers := util.GetProviderName(normalizedModel)

        // [MỚI] Check thêm: nếu model match với claude-api-key aliases, coi như có provider
        if len(providers) == 0 && fh.hasClaudeAPIKeyAlias(modelName) {
            log.Infof("amp fallback: model %s matched claude-api-key alias, using local provider", modelName)
            providers = []string{"claude"}
        }

        if len(providers) == 0 {
            // Fallback to ampcode.com (chỉ khi KHÔNG match alias)
            // ...
        }

        // Có providers → gọi handler local
        handler(c)
    }
}
```

#### 4.1.5 Thêm function hasClaudeAPIKeyAlias

```go
// hasClaudeAPIKeyAlias checks if the model name matches any alias in claude-api-key config
// Hàm này kiểm tra xem model có match với aliases trong config.yaml không
// Ví dụ: claude-haiku-4-5-20251001 có thể được map sang claude-haiku-4-5 (Azure)
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
```

---

### 4.2 `internal/api/modules/amp/amp.go`

#### 4.2.1 Thêm fields vào struct AmpModule

```go
type AmpModule struct {
    secretSource    SecretSource
    proxy           *httputil.ReverseProxy
    accessManager   *sdkaccess.Manager
    authMiddleware_ gin.HandlerFunc
    enabled         bool
    registerOnce    sync.Once
    cfg             *config.Config  // [MỚI] Lưu config để FallbackHandler có thể check aliases
    cfgMu           sync.RWMutex    // [MỚI] Mutex bảo vệ config access
}
```

#### 4.2.2 Sửa method Register

```go
func (m *AmpModule) Register(ctx modules.Context) error {
    // [MỚI] Lưu config reference để FallbackHandler có thể check claude-api-key aliases
    m.cfgMu.Lock()
    m.cfg = ctx.Config
    m.cfgMu.Unlock()

    upstreamURL := strings.TrimSpace(ctx.Config.AmpUpstreamURL)
    // ... rest of existing code ...
}
```

#### 4.2.3 Sửa method OnConfigUpdated

```go
func (m *AmpModule) OnConfigUpdated(cfg *config.Config) error {
    // [MỚI] Update config reference để FallbackHandler có thể check claude-api-key aliases mới
    m.cfgMu.Lock()
    m.cfg = cfg
    m.cfgMu.Unlock()

    // ... rest of existing code ...
}
```

#### 4.2.4 Thêm method GetConfig

```go
// GetConfig returns the current config (thread-safe)
// [AZURE-CLAUDE] Dùng để FallbackHandler có thể check claude-api-key aliases
func (m *AmpModule) GetConfig() *config.Config {
    m.cfgMu.RLock()
    defer m.cfgMu.RUnlock()
    return m.cfg
}
```

---

### 4.3 `internal/api/modules/amp/routes.go`

#### 4.3.1 Thêm import

```go
import (
    // ... existing imports ...
    "github.com/router-for-me/CLIProxyAPI/v6/internal/config"  // [MỚI]
)
```

#### 4.3.2 Sửa function registerProviderAliases

```go
func (m *AmpModule) registerProviderAliases(engine *gin.Engine, baseHandler *handlers.BaseAPIHandler, auth gin.HandlerFunc) {
    // ... existing handler creation ...

    // [MỚI] Sử dụng NewFallbackHandlerWithConfig để có thể check claude-api-key aliases
    fallbackHandler := NewFallbackHandlerWithConfig(
        func() *httputil.ReverseProxy {
            return m.proxy
        },
        func() *config.Config {
            return m.GetConfig()
        },
    )

    // ... rest of route registration ...
}
```

---

## 5. Cấu hình config.yaml

### 5.1 Cấu trúc claude-api-key

```yaml
# [AZURE-CLAUDE] Azure Foundry Anthropic endpoint
# Amp CLI sẽ được ép dùng Azure Claude thay vì fallback sang ampcode.com
claude-api-key:
  - api-key: "<AZURE_FOUNDRY_API_KEY>"
    base-url: "https://foundry-sonnet.services.ai.azure.com/anthropic"
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "<AZURE_FOUNDRY_API_KEY>"
    models:
      # Map Amp CLI model names -> Azure Foundry model names
      # Format: name = Azure model, alias = Amp CLI model
      
      # Haiku models
      - name: "claude-haiku-4-5"
        alias: "claude-haiku-4-5-20251001"
      - name: "claude-haiku-4-5"
        alias: "claude-haiku-4-5"
      
      # Sonnet models  
      - name: "claude-sonnet-4-5"
        alias: "claude-sonnet-4-5-20250929"
      - name: "claude-sonnet-4-5"
        alias: "claude-sonnet-4-5"
      
      # Opus models
      - name: "claude-opus-4-5"
        alias: "claude-opus-4-5-20251101"
      - name: "claude-opus-4-5"
        alias: "claude-opus-4-5"
      - name: "claude-opus-4-1"
        alias: "claude-opus-4-1"
```

### 5.2 Giải thích các trường

| Trường | Mô tả |
|--------|-------|
| `api-key` | API key từ Azure Foundry |
| `base-url` | Endpoint của Azure Foundry (bao gồm `/anthropic`) |
| `headers` | Headers bắt buộc cho Azure Foundry |
| `models` | Danh sách model aliases |
| `models[].name` | Model name thực tế trên Azure Foundry |
| `models[].alias` | Model name mà Amp CLI gửi đến |

### 5.3 Cấu hình môi trường Azure Foundry tương ứng

```bash
export CLAUDE_CODE_USE_FOUNDRY=1
export ANTHROPIC_FOUNDRY_RESOURCE="foundry-sonnet"
export ANTHROPIC_FOUNDRY_API_KEY="<your-api-key>"
export ANTHROPIC_DEFAULT_OPUS_MODEL="claude-opus-4-5"
export ANTHROPIC_DEFAULT_SONNET_MODEL="claude-sonnet-4-5"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="claude-haiku-4-5"
```

---

## 6. Hướng dẫn build và triển khai

### 6.1 Yêu cầu

- Go 1.21+ (hoặc mới hơn)
- Git

### 6.2 Clone và checkout branch

```bash
cd /home/azureuser/cliproxyapi
git clone https://github.com/router-for-me/CLIProxyAPI.git
cd CLIProxyAPI
git checkout -b feature/azure-claude-provider
```

### 6.3 Áp dụng các thay đổi

Sửa các file theo hướng dẫn ở [Mục 4](#4-chi-tiết-các-file-đã-sửa-đổi):

1. `internal/api/modules/amp/fallback_handlers.go`
2. `internal/api/modules/amp/amp.go`
3. `internal/api/modules/amp/routes.go`

### 6.4 Build

```bash
cd /home/azureuser/cliproxyapi/CLIProxyAPI
go build -o cli-proxy-api-azure-claude ./cmd/server
```

### 6.5 Copy binary và cấu hình

```bash
cp cli-proxy-api-azure-claude ../
cd ..
```

### 6.6 Cập nhật config.yaml

Sửa `/home/azureuser/cliproxyapi/config.yaml` theo [Mục 5](#5-cấu-hình-configyaml).

### 6.7 Chạy

```bash
cd /home/azureuser/cliproxyapi
./cli-proxy-api-azure-claude --config config.yaml
```

---

## 7. Kiểm thử

### 7.1 Test trực tiếp Azure Foundry

```bash
curl -s "https://foundry-sonnet.services.ai.azure.com/anthropic/v1/messages" \
  -H "x-api-key: <API_KEY>" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"Hi"}]}'
```

**Expected**: Response từ Claude

### 7.2 Test qua CLIProxyAPI với Amp model name

```bash
curl -s http://localhost:8317/api/provider/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-opus-4-5-20251101","max_tokens":10,"messages":[{"role":"user","content":"Test"}]}'
```

**Expected**: 
- Response từ Claude (qua Azure Foundry)
- Log hiển thị: `amp fallback: model claude-opus-4-5-20251101 matched claude-api-key alias, using local provider`

### 7.3 Test với Amp CLI

```bash
# Cấu hình Amp CLI dùng proxy
# ~/.config/amp/settings.json
{
  "amp.url": "http://localhost:8317"
}

# Chạy Amp CLI
amp chat "Hello"
```

**Expected**: Amp CLI nhận response từ Azure Claude (không còn lỗi 402 Out of credits)

---

## 8. Troubleshooting

### 8.1 Lỗi "DeploymentNotFound"

**Nguyên nhân**: Model name trong `config.yaml` không khớp với deployment trên Azure Foundry.

**Giải pháp**: 
1. Kiểm tra Azure portal để xem deployment names
2. Cập nhật `models[].name` trong `config.yaml`

### 8.2 Request vẫn fallback về ampcode.com

**Nguyên nhân**: Model name không match với alias nào trong config.

**Giải pháp**:
1. Check log để xem model name thực tế Amp CLI gửi
2. Thêm alias tương ứng vào `config.yaml`
3. Restart CLIProxyAPI

### 8.3 Lỗi "Missing API key" từ Azure

**Nguyên nhân**: Headers không được gửi đúng.

**Giải pháp**: Đảm bảo `config.yaml` có:
```yaml
headers:
  anthropic-version: "2023-06-01"
  x-api-key: "<API_KEY>"
```

### 8.4 Lỗi build "redeclared in this block"

**Nguyên nhân**: Function bị duplicate (ví dụ `filterBetaFeatures`).

**Giải pháp**: Xóa function duplicate khỏi file vừa sửa (giữ bản gốc trong file khác).

---

## Phụ lục

### A. Danh sách file đã sửa đổi

| File | Dòng sửa | Mô tả |
|------|----------|-------|
| `internal/api/modules/amp/fallback_handlers.go` | +import, +struct field, +function | Thêm logic check aliases |
| `internal/api/modules/amp/amp.go` | +struct fields, +methods | Lưu và expose config |
| `internal/api/modules/amp/routes.go` | +import, sửa constructor call | Truyền config vào FallbackHandler |

### B. Model mapping reference

| Amp CLI Model | Azure Foundry Model |
|---------------|---------------------|
| `claude-haiku-4-5-20251001` | `claude-haiku-4-5` |
| `claude-sonnet-4-5-20250929` | `claude-sonnet-4-5` |
| `claude-opus-4-5-20251101` | `claude-opus-4-5` |
| `claude-opus-4-1` | `claude-opus-4-1` |

### C. Commit message mẫu

```
feat(amp): Add Azure Claude integration via claude-api-key aliases

- Add hasClaudeAPIKeyAlias() to check config aliases before fallback
- Store config reference in AmpModule for FallbackHandler access
- Use NewFallbackHandlerWithConfig() to enable alias checking
- Update config.yaml with Azure Foundry endpoint and model mappings

This allows Amp CLI to use Azure Foundry Claude instead of falling
back to ampcode.com when OAuth tokens are not available.
```

---

*Tài liệu này được tạo tự động và có thể được cập nhật khi có thay đổi mới.*
