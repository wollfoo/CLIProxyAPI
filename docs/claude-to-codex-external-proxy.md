# Claude-to-Codex External Proxy (Giải pháp khuyên dùng)

Hướng dẫn kỹ thuật sử dụng proxy bên ngoài để chuyển đổi request từ Claude API sang Codex (GPT-5.1), kết hợp với CLIProxyAPI để Amp CLI sử dụng GPT-5.1 làm model Smart mặc định.

## Mục lục

- [Tổng quan](#tổng-quan)
- [So sánh với giải pháp tự code](#so-sánh-với-giải-pháp-tự-code)
- [Kiến trúc](#kiến-trúc)
- [Các project khuyên dùng](#các-project-khuyên-dùng)
- [Triển khai với claude-code-provider-proxy](#triển-khai-với-claude-code-provider-proxy)
- [Triển khai với ccproxy-api](#triển-khai-với-ccproxy-api)
- [Tích hợp với CLIProxyAPI](#tích-hợp-với-cliproxyapi)
- [Cấu hình Amp CLI](#cấu-hình-amp-cli)
- [Testing](#testing)
- [Troubleshooting](#troubleshooting)
- [Tham khảo](#tham-khảo)

---

## Tổng quan

### Ý tưởng

Thay vì sửa source code CLIProxyAPI, sử dụng một **proxy translator bên ngoài** để:

1. Nhận request Claude format từ Amp CLI
2. Translate sang OpenAI format
3. Forward đến CLIProxyAPI (Codex endpoint)
4. Translate response ngược lại Claude format
5. Trả về cho Amp CLI

### Ưu điểm

| Ưu điểm | Mô tả |
|---------|-------|
| **Không sửa code** | CLIProxyAPI giữ nguyên, không cần rebuild |
| **Dễ cập nhật** | Cập nhật CLIProxyAPI độc lập với translator |
| **Modular** | Bật/tắt bằng cách start/stop proxy |
| **Community support** | Sử dụng project đã được test bởi cộng đồng |
| **Dễ rollback** | Gặp vấn đề → tắt proxy → Amp dùng Claude trực tiếp |

### Nhược điểm

| Nhược điểm | Mô tả |
|------------|-------|
| **Thêm hop** | Request đi qua thêm 1 layer (tăng latency ~5-20ms) |
| **Phức tạp deploy** | Cần quản lý 2 services thay vì 1 |
| **Port management** | Cần allocate thêm port cho translator proxy |

---

## So sánh với giải pháp tự code

| Tiêu chí | External Proxy | Tự code translator |
|----------|----------------|-------------------|
| **Độ phức tạp** | Thấp (config only) | Cao (Go programming) |
| **Thời gian triển khai** | 30 phút | 2-4 giờ |
| **Maintenance** | Cộng đồng maintain | Tự maintain |
| **Performance** | +5-20ms latency | Native (không overhead) |
| **Customization** | Giới hạn | Hoàn toàn tùy chỉnh |
| **CLIProxyAPI updates** | Không ảnh hưởng | Có thể cần sync |

**Khuyến nghị**: Bắt đầu với External Proxy. Nếu cần tùy chỉnh sâu hoặc optimize latency, chuyển sang tự code.

---

## Kiến trúc

### Tổng quan hệ thống

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AMP CLI                                         │
│                     (Claude format request)                                  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    EXTERNAL TRANSLATOR PROXY                                 │
│                         (Port 8318)                                          │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  /v1/messages (Claude endpoint)                                       │  │
│  │       │                                                               │  │
│  │       ▼                                                               │  │
│  │  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐   │  │
│  │  │ Request Parser  │───▶│ Claude→OpenAI   │───▶│ Forward to      │   │  │
│  │  │ (Claude format) │    │ Translator      │    │ CLIProxyAPI     │   │  │
│  │  └─────────────────┘    └─────────────────┘    └─────────────────┘   │  │
│  │                                                        │              │  │
│  │                                                        ▼              │  │
│  │  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐   │  │
│  │  │ Response to     │◀───│ OpenAI→Claude   │◀───│ Response from   │   │  │
│  │  │ Amp CLI         │    │ Translator      │    │ CLIProxyAPI     │   │  │
│  │  └─────────────────┘    └─────────────────┘    └─────────────────┘   │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CLIProxyAPI                                        │
│                            (Port 8317)                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  /v1/chat/completions (OpenAI/Codex endpoint)                         │  │
│  │       │                                                               │  │
│  │       ▼                                                               │  │
│  │  ┌─────────────────┐                                                  │  │
│  │  │ Codex Executor  │──────────────────────────────────────────────▶   │  │
│  │  │ (OAuth token)   │                          GPT-5.1 API             │  │
│  │  └─────────────────┘                                                  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Luồng request chi tiết

```
1. Amp CLI gửi request:
   POST http://localhost:8318/v1/messages
   {
     "model": "claude-opus-4-5",
     "messages": [{"role": "user", "content": "Hello"}]
   }

2. Translator Proxy nhận và translate:
   POST http://localhost:8317/v1/chat/completions
   {
     "model": "gpt-5.1",
     "messages": [{"role": "user", "content": "Hello"}]
   }

3. CLIProxyAPI forward đến Codex (GPT-5.1):
   POST https://api.openai.com/v1/chat/completions
   (với OAuth token)

4. Response được translate ngược:
   OpenAI format → Claude format

5. Amp CLI nhận response Claude format
```

---

## Các project khuyên dùng

### 1. claude-code-provider-proxy (⭐ Khuyên dùng nhất)

| Thông tin | Chi tiết |
|-----------|----------|
| **GitHub** | https://github.com/ujisati/claude-code-provider-proxy |
| **Language** | TypeScript/Node.js |
| **Features** | Full Claude ↔ OpenAI translation, streaming support |
| **Maintenance** | Active |

### 2. ccproxy-api

| Thông tin | Chi tiết |
|-----------|----------|
| **GitHub** | https://github.com/CaddyGlow/ccproxy-api |
| **Language** | Go |
| **Features** | Translation layer, instruction prompt injection |
| **Maintenance** | Active |

### 3. claude-code-proxy

| Thông tin | Chi tiết |
|-----------|----------|
| **GitHub** | https://github.com/fuergaosi233/claude-code-proxy |
| **Language** | TypeScript |
| **Features** | Multiple provider support, Azure OpenAI |
| **Maintenance** | Active |

---

## Triển khai với claude-code-provider-proxy

### Yêu cầu

- Node.js 18+
- npm hoặc yarn
- CLIProxyAPI đang chạy với Codex OAuth

### Bước 1: Clone và cài đặt

```bash
cd /home/azureuser
git clone https://github.com/ujisati/claude-code-provider-proxy.git
cd claude-code-provider-proxy

# Cài đặt dependencies
npm install
```

### Bước 2: Cấu hình

Tạo file `.env`:

```bash
# .env file
# Endpoint của CLIProxyAPI (Codex)
OPENAI_BASE_URL=http://localhost:8317/v1

# API key (để trống nếu CLIProxyAPI không yêu cầu)
OPENAI_API_KEY=dummy-key

# Model mapping
DEFAULT_MODEL=gpt-5.1

# Port cho translator proxy
PORT=8318

# Enable streaming
STREAM_ENABLED=true

# Logging
LOG_LEVEL=info
```

Hoặc tạo file `config.json`:

```json
{
  "port": 8318,
  "target": {
    "baseUrl": "http://localhost:8317/v1",
    "apiKey": "dummy-key"
  },
  "modelMapping": {
    "claude-opus-4-5-20251101": "gpt-5.1",
    "claude-opus-4-5": "gpt-5.1",
    "claude-opus-4-1": "gpt-5.1",
    "claude-sonnet-4-5-20250929": "gpt-5.1-mini",
    "claude-sonnet-4-5": "gpt-5.1-mini",
    "claude-haiku-4-5-20251001": "gpt-5-turbo",
    "claude-haiku-4-5": "gpt-5-turbo"
  },
  "defaultModel": "gpt-5.1",
  "logging": {
    "level": "info",
    "requests": true,
    "responses": false
  }
}
```

### Bước 3: Khởi động

```bash
# Development mode
npm run dev

# Production mode
npm run build
npm start

# Hoặc với PM2 (khuyến nghị cho production)
npm install -g pm2
pm2 start npm --name "claude-codex-proxy" -- start
pm2 save
```

### Bước 4: Verify

```bash
# Test endpoint
curl http://localhost:8318/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-5",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Say hello"}]
  }'

# Expected: Response in Claude format, but powered by GPT-5.1
```

---

## Triển khai với ccproxy-api

### Yêu cầu

- Go 1.21+
- CLIProxyAPI đang chạy với Codex OAuth

### Bước 1: Clone và build

```bash
cd /home/azureuser
git clone https://github.com/CaddyGlow/ccproxy-api.git
cd ccproxy-api

# Build
go build -o ccproxy-api ./cmd/main.go
```

### Bước 2: Cấu hình

Tạo file `config.yaml`:

```yaml
# ccproxy-api config
server:
  port: 8318
  host: "0.0.0.0"

# Target: CLIProxyAPI Codex endpoint
upstream:
  base_url: "http://localhost:8317/v1"
  api_key: ""  # Để trống nếu không cần

# Model mapping
models:
  default: "gpt-5.1"
  mapping:
    claude-opus-4-5-20251101: "gpt-5.1"
    claude-opus-4-5: "gpt-5.1"
    claude-sonnet-4-5: "gpt-5.1-mini"
    claude-haiku-4-5: "gpt-5-turbo"

# Translation settings
translation:
  # Inject system prompt (optional)
  system_prompt: ""
  # Max tokens default
  max_tokens: 8192

# Logging
logging:
  level: "info"
  format: "json"
```

### Bước 3: Khởi động

```bash
# Foreground
./ccproxy-api --config config.yaml

# Background với systemd
sudo tee /etc/systemd/system/ccproxy-api.service > /dev/null <<EOF
[Unit]
Description=Claude-Codex Proxy API
After=network.target

[Service]
Type=simple
User=azureuser
WorkingDirectory=/home/azureuser/ccproxy-api
ExecStart=/home/azureuser/ccproxy-api/ccproxy-api --config config.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable ccproxy-api
sudo systemctl start ccproxy-api
```

---

## Tích hợp với CLIProxyAPI

### Kiến trúc tổng thể

```
┌────────────────────────────────────────────────────────────────────────┐
│                         DEPLOYMENT ARCHITECTURE                         │
├────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────────┐                                                   │
│  │    Amp CLI      │                                                   │
│  │  amp.url=:8318  │                                                   │
│  └────────┬────────┘                                                   │
│           │                                                            │
│           │ Claude API (/v1/messages)                                  │
│           ▼                                                            │
│  ┌─────────────────┐                                                   │
│  │ Translator      │ ◀── Port 8318                                     │
│  │ Proxy           │     (claude-code-provider-proxy)                  │
│  └────────┬────────┘                                                   │
│           │                                                            │
│           │ OpenAI API (/v1/chat/completions)                          │
│           ▼                                                            │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                      CLIProxyAPI                                │   │
│  │                      Port 8317                                  │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │   │
│  │  │   Gemini    │  │   Codex     │  │   Claude    │             │   │
│  │  │  (OAuth)    │  │  (OAuth)    │  │  (Azure)    │             │   │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘             │   │
│  └─────────┼────────────────┼────────────────┼─────────────────────┘   │
│            │                │                │                          │
│            ▼                ▼                ▼                          │
│     Google Gemini     OpenAI GPT-5.1   Azure Claude                    │
│                                                                         │
└────────────────────────────────────────────────────────────────────────┘
```

### CLIProxyAPI config (config.yaml)

```yaml
# =============================================================================
# CLIProxyAPI Configuration
# Kết hợp với External Translator Proxy
# =============================================================================

port: 8317
auth-dir: "~/.cli-proxy-api"

# Amp upstream (cho management routes)
amp-upstream-url: "https://ampcode.com"
amp-restrict-management-to-localhost: true

# Debug logging
debug: true
logging-to-file: true

# =============================================================================
# PROVIDER 1: CODEX (OAuth) - QUAN TRỌNG cho translator proxy
# =============================================================================
# Đảm bảo đã login: ./cli-proxy-api --codex-login
# Token file: ~/.cli-proxy-api/codex-*.json

# Không cần config thêm - OAuth token tự động load từ auth-dir

# =============================================================================
# PROVIDER 2: GEMINI (OAuth) - Cho các subagent khác
# =============================================================================
# Đảm bảo đã login: ./cli-proxy-api --login

# =============================================================================
# PROVIDER 3: CLAUDE (Azure) - Backup/fallback
# =============================================================================
# Giữ lại để fallback nếu cần
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "YOUR_AZURE_API_KEY"
    models:
      - name: "claude-haiku-4-5"
        alias: "claude-haiku-4-5-20251001"
      - name: "claude-sonnet-4-5"
        alias: "claude-sonnet-4-5-20250929"
      - name: "claude-opus-4-5"
        alias: "claude-opus-4-5-20251101"
```

### Startup script

Tạo file `/home/azureuser/start-amp-codex.sh`:

```bash
#!/bin/bash

# =============================================================================
# Startup Script: CLIProxyAPI + Translator Proxy
# Cho phép Amp CLI dùng GPT-5.1 (Codex) thay vì Claude
# =============================================================================

set -e

CLIPROXY_DIR="/home/azureuser/cliproxyapi/CLIProxyAPI"
TRANSLATOR_DIR="/home/azureuser/claude-code-provider-proxy"

echo "=============================================="
echo "Starting Amp CLI with Codex (GPT-5.1) backend"
echo "=============================================="

# Check if CLIProxyAPI is already running
if pgrep -f "cli-proxy-api" > /dev/null; then
    echo "[INFO] CLIProxyAPI already running"
else
    echo "[INFO] Starting CLIProxyAPI on port 8317..."
    cd "$CLIPROXY_DIR"
    nohup ./cli-proxy-api --config config.yaml > run.log 2>&1 &
    sleep 2
    
    if pgrep -f "cli-proxy-api" > /dev/null; then
        echo "[OK] CLIProxyAPI started"
    else
        echo "[ERROR] Failed to start CLIProxyAPI"
        exit 1
    fi
fi

# Check if Translator Proxy is already running
if pgrep -f "claude-code-provider-proxy" > /dev/null || lsof -i:8318 > /dev/null 2>&1; then
    echo "[INFO] Translator Proxy already running on port 8318"
else
    echo "[INFO] Starting Translator Proxy on port 8318..."
    cd "$TRANSLATOR_DIR"
    nohup npm start > translator.log 2>&1 &
    sleep 3
    
    if lsof -i:8318 > /dev/null 2>&1; then
        echo "[OK] Translator Proxy started"
    else
        echo "[ERROR] Failed to start Translator Proxy"
        exit 1
    fi
fi

echo ""
echo "=============================================="
echo "Services Running:"
echo "  - CLIProxyAPI:      http://localhost:8317"
echo "  - Translator Proxy: http://localhost:8318"
echo ""
echo "Configure Amp CLI:"
echo "  export AMP_URL=http://localhost:8318"
echo "  OR edit ~/.config/amp/settings.json:"
echo '  { "amp.url": "http://localhost:8318" }'
echo "=============================================="

# Verify connectivity
echo ""
echo "Verifying connectivity..."

# Test CLIProxyAPI
if curl -s http://localhost:8317/v1/models > /dev/null; then
    echo "[OK] CLIProxyAPI responding"
else
    echo "[WARN] CLIProxyAPI not responding"
fi

# Test Translator Proxy
if curl -s http://localhost:8318/health > /dev/null 2>&1 || curl -s -o /dev/null -w "%{http_code}" http://localhost:8318/ | grep -q "200\|404"; then
    echo "[OK] Translator Proxy responding"
else
    echo "[WARN] Translator Proxy may not be responding correctly"
fi

echo ""
echo "Ready! Use: amp \"Your prompt here\""
```

```bash
chmod +x /home/azureuser/start-amp-codex.sh
```

### Stop script

Tạo file `/home/azureuser/stop-amp-codex.sh`:

```bash
#!/bin/bash

echo "Stopping services..."

# Stop Translator Proxy
if pgrep -f "claude-code-provider-proxy" > /dev/null; then
    pkill -f "claude-code-provider-proxy"
    echo "[OK] Translator Proxy stopped"
fi

# Optionally stop CLIProxyAPI (comment out if you want to keep it running)
# if pgrep -f "cli-proxy-api" > /dev/null; then
#     pkill -f "cli-proxy-api"
#     echo "[OK] CLIProxyAPI stopped"
# fi

echo "Done. To use Amp with Claude again:"
echo "  export AMP_URL=http://localhost:8317"
```

```bash
chmod +x /home/azureuser/stop-amp-codex.sh
```

---

## Cấu hình Amp CLI

### Option 1: Environment variable

```bash
# Dùng Translator Proxy (GPT-5.1)
export AMP_URL=http://localhost:8318

# Quay lại CLIProxyAPI trực tiếp (Claude)
export AMP_URL=http://localhost:8317

# Quay lại ampcode.com gốc
unset AMP_URL
```

### Option 2: Settings file

Sửa file `~/.config/amp/settings.json`:

```json
{
  // Dùng Translator Proxy → GPT-5.1
  "amp.url": "http://localhost:8318",
  
  // Các settings khác giữ nguyên
  "amp.anthropic.thinking.enabled": true,
  "amp.permissions": []
}
```

### Option 3: Workspace-specific

Tạo file `.amp/settings.json` trong project directory:

```json
{
  // Chỉ project này dùng GPT-5.1
  "amp.url": "http://localhost:8318"
}
```

---

## Testing

### 1. Test từng component

```bash
# 1. Test CLIProxyAPI Codex endpoint
curl http://localhost:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.1",
    "messages": [{"role": "user", "content": "What model are you?"}],
    "max_tokens": 50
  }'

# Expected: Response from GPT-5.1

# 2. Test Translator Proxy
curl http://localhost:8318/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-5",
    "max_tokens": 50,
    "messages": [{"role": "user", "content": "What model are you?"}]
  }'

# Expected: Claude-format response, powered by GPT-5.1
```

### 2. Test với Amp CLI

```bash
# Set Amp URL to Translator Proxy
export AMP_URL=http://localhost:8318

# Simple test
amp "What AI model are you using right now? Be specific."

# Check logs
tail -f /home/azureuser/claude-code-provider-proxy/translator.log
tail -f /home/azureuser/cliproxyapi/CLIProxyAPI/run.log
```

### 3. Test streaming

```bash
curl http://localhost:8318/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-5",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "Count from 1 to 10 slowly"}]
  }'

# Expected: SSE stream in Claude format
```

### 4. Test tool calling

```bash
curl http://localhost:8318/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: dummy" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-5",
    "max_tokens": 500,
    "messages": [{"role": "user", "content": "What is the weather in Tokyo?"}],
    "tools": [{
      "name": "get_weather",
      "description": "Get weather for a location",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string"}
        },
        "required": ["location"]
      }
    }]
  }'

# Expected: Tool use response in Claude format
```

---

## Troubleshooting

### Common Issues

| Vấn đề | Nguyên nhân | Giải pháp |
|--------|-------------|-----------|
| Connection refused :8318 | Translator proxy không chạy | `./start-amp-codex.sh` |
| Connection refused :8317 | CLIProxyAPI không chạy | Start CLIProxyAPI |
| 401 Unauthorized | Codex OAuth expired | `./cli-proxy-api --codex-login` |
| Model not found | Model mapping sai | Kiểm tra config translator |
| Streaming bị đứt | SSE translation lỗi | Check translator logs |
| Response format sai | Translation incomplete | Update translator version |

### Debug commands

```bash
# Check running processes
ps aux | grep -E "(cli-proxy-api|node|ccproxy)"

# Check ports
lsof -i:8317
lsof -i:8318

# View CLIProxyAPI logs
tail -100 /home/azureuser/cliproxyapi/CLIProxyAPI/run.log

# View Translator logs
tail -100 /home/azureuser/claude-code-provider-proxy/translator.log

# Test connectivity
curl -v http://localhost:8317/v1/models
curl -v http://localhost:8318/health
```

### Verify model được sử dụng

```bash
# Enable debug trong CLIProxyAPI
# config.yaml: debug: true

# Sau đó xem log:
tail -f run.log | grep -E "(model|gpt-5|codex)"

# Expected output khi có request:
# [debug] Use API key ... for model gpt-5.1
# [info] POST /v1/chat/completions ...
```

---

## Chuyển đổi giữa Claude và Codex

### Script chuyển đổi nhanh

Tạo file `/home/azureuser/switch-amp-backend.sh`:

```bash
#!/bin/bash

case "$1" in
  codex|gpt)
    echo "Switching to Codex (GPT-5.1)..."
    export AMP_URL=http://localhost:8318
    echo "AMP_URL=$AMP_URL"
    echo "Using: GPT-5.1 via Translator Proxy"
    ;;
  claude)
    echo "Switching to Claude..."
    export AMP_URL=http://localhost:8317
    echo "AMP_URL=$AMP_URL"
    echo "Using: Claude via CLIProxyAPI (Azure)"
    ;;
  direct|ampcode)
    echo "Switching to ampcode.com direct..."
    unset AMP_URL
    echo "AMP_URL unset"
    echo "Using: ampcode.com default"
    ;;
  status)
    echo "Current AMP_URL: ${AMP_URL:-'(not set - using ampcode.com)'}"
    ;;
  *)
    echo "Usage: source switch-amp-backend.sh [codex|claude|direct|status]"
    echo ""
    echo "  codex   - Use GPT-5.1 via Translator Proxy (port 8318)"
    echo "  claude  - Use Claude via CLIProxyAPI (port 8317)"
    echo "  direct  - Use ampcode.com directly"
    echo "  status  - Show current backend"
    ;;
esac
```

```bash
# Usage (note: use 'source' to affect current shell)
source /home/azureuser/switch-amp-backend.sh codex
source /home/azureuser/switch-amp-backend.sh claude
source /home/azureuser/switch-amp-backend.sh status
```

---

## Performance Tuning

### Giảm latency

```json
// Translator config
{
  "connection": {
    "keepAlive": true,
    "timeout": 30000,
    "maxSockets": 100
  },
  "cache": {
    "enabled": false  // Disable nếu không cần
  }
}
```

### Resource limits

```yaml
# Nếu dùng systemd
# /etc/systemd/system/claude-codex-proxy.service
[Service]
MemoryMax=512M
CPUQuota=50%
```

---

## Tham khảo

### Documentation

- [claude-code-provider-proxy README](https://github.com/ujisati/claude-code-provider-proxy)
- [ccproxy-api Documentation](https://github.com/CaddyGlow/ccproxy-api)
- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)

### Related Docs

- [CLIProxyAPI Amp Integration](amp-cli-integration.md)
- [Custom Claude-to-Codex Translator](custom-claude-to-codex-translator.md) - Giải pháp tự code

---

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2025-11-27 | Initial documentation |

---

## Disclaimer

Việc sử dụng proxy để chuyển đổi API format có thể vi phạm Terms of Service của các nhà cung cấp. Người dùng tự chịu trách nhiệm về việc sử dụng. Tài liệu này chỉ mang tính chất kỹ thuật và giáo dục.
