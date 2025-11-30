# Đề xuất phương án: Redirect Oracle GPT-5 → Claude Opus 4.5 (Azure)

> **Tài liệu**: Hướng dẫn kỹ thuật redirect Amp CLI Oracle từ GPT-5 sang Claude Opus 4.5 trên Azure AI Foundry  
> **Ngày tạo**: 2025-11-30  
> **Phiên bản**: 1.0

---

## Mục lục

- [Bối cảnh](#bối-cảnh)
- [Vấn đề kỹ thuật](#vấn-đề-kỹ-thuật)
- [Giải pháp: Sử dụng Protocol Translator](#giải-pháp-sử-dụng-protocol-translator)
- [Phương án 1: OpenAI-to-Claude Translator (Node.js)](#phương-án-1-openai-to-claude-translator-nodejs)
- [Phương án 2: ccproxy-api (Go-based)](#phương-án-2-ccproxy-api-go-based)
- [Tóm tắt luồng hoạt động](#tóm-tắt-luồng-hoạt-động)
- [Kết luận](#kết-luận)

---

## Bối cảnh

Amp CLI sử dụng **Oracle subagent** để xử lý các tác vụ phức tạp như:
- Complex reasoning
- Code review
- Debugging
- Architecture analysis

Theo mặc định, Oracle sử dụng **GPT-5.1** của OpenAI. Tuy nhiên, trong một số trường hợp, người dùng muốn redirect Oracle để sử dụng **Claude Opus 4.5** trên Azure AI Foundry thay vì GPT-5.

### Lý do redirect

1. **Chi phí**: Sử dụng Azure credits thay vì OpenAI API costs
2. **Bảo mật**: Dữ liệu đi qua Azure enterprise environment
3. **Hiệu năng**: Claude Opus có thể phù hợp hơn cho một số use cases
4. **Thống nhất**: Toàn bộ workflow sử dụng Claude models

---

## Vấn đề kỹ thuật

### Oracle gọi API như thế nào?

Khi Amp CLI gọi **Oracle**:

| Thuộc tính | Giá trị |
|------------|---------|
| **Endpoint** | `/api/provider/openai/v1/chat/completions` |
| **Model** | `gpt-5` hoặc `gpt-5.1` |
| **Protocol** | OpenAI Chat Completions API |

### Azure AI Foundry Claude API

| Thuộc tính | Giá trị |
|------------|---------|
| **Endpoint** | `https://<resource>.services.ai.azure.com/anthropic/v1/messages` |
| **Model** | `claude-opus-4-5` |
| **Protocol** | Anthropic Messages API |

### Tại sao không thể redirect trực tiếp?

**OpenAI Chat Completions Request:**
```json
{
  "model": "gpt-5",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello"}
  ],
  "max_tokens": 1000,
  "temperature": 0.7
}
```

**Anthropic Messages Request:**
```json
{
  "model": "claude-opus-4-5",
  "system": "You are a helpful assistant.",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "max_tokens": 1000,
  "temperature": 0.7
}
```

→ **Format hoàn toàn khác nhau** - cần protocol translator.

---

## Giải pháp: Sử dụng Protocol Translator

### Kiến trúc tổng quan

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              GIẢI PHÁP ĐỀ XUẤT                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Amp CLI (Oracle)                                                           │
│       │                                                                     │
│       │ OpenAI Protocol: POST /v1/chat/completions                          │
│       │ model: "gpt-5"                                                      │
│       ▼                                                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ CLIProxyAPI (port 8317)                                             │   │
│  │                                                                     │   │
│  │ codex-api-key:                                                      │   │
│  │   - base-url: "http://localhost:8318/v1"  ◀── Translator Proxy      │   │
│  │     models:                                                         │   │
│  │       - name: "claude-opus-4-5"                                     │   │
│  │         alias: "gpt-5"                                              │   │
│  └──────────────────────────┬──────────────────────────────────────────┘   │
│                             │                                               │
│                             │ OpenAI Protocol (model remapped)              │
│                             ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ Translator Proxy (port 8318)                                        │   │
│  │ (openai-to-claude-translator)                                       │   │
│  │                                                                     │   │
│  │ Chức năng:                                                          │   │
│  │   1. Nhận request OpenAI format                                     │   │
│  │   2. Convert sang Anthropic Claude format                           │   │
│  │   3. Forward tới Azure AI Foundry                                   │   │
│  └──────────────────────────┬──────────────────────────────────────────┘   │
│                             │                                               │
│                             │ Anthropic Protocol: POST /v1/messages         │
│                             ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ Azure AI Foundry                                                    │   │
│  │ https://YOUR_RESOURCE.services.ai.azure.com/anthropic               │   │
│  │                                                                     │   │
│  │ model: claude-opus-4-5                                              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Phương án 1: OpenAI-to-Claude Translator (Node.js)

### Ưu điểm

- Dễ customize
- Nhẹ, nhanh
- Dễ debug

### Bước 1: Tạo thư mục project

```bash
cd /home/azureuser
mkdir -p openai-to-claude-translator
cd openai-to-claude-translator
```

### Bước 2: Tạo package.json

```json
{
  "name": "openai-to-claude-translator",
  "version": "1.0.0",
  "description": "Translates OpenAI Chat Completions API to Anthropic Messages API",
  "main": "index.js",
  "scripts": {
    "start": "node index.js",
    "dev": "nodemon index.js"
  },
  "dependencies": {
    "express": "^4.18.2",
    "axios": "^1.6.0"
  },
  "devDependencies": {
    "nodemon": "^3.0.0"
  }
}
```

### Bước 3: Tạo index.js

```javascript
// OpenAI → Claude Protocol Translator
// Converts OpenAI Chat Completions API to Anthropic Messages API

const express = require('express');
const axios = require('axios');

const app = express();
app.use(express.json({ limit: '50mb' }));

// Azure AI Foundry Configuration
const AZURE_ENDPOINT = process.env.AZURE_ENDPOINT || 'https://YOUR_RESOURCE.services.ai.azure.com/anthropic';
const AZURE_API_KEY = process.env.AZURE_API_KEY || 'YOUR_AZURE_API_KEY';
const DEFAULT_MODEL = process.env.DEFAULT_MODEL || 'claude-opus-4-5';

// Logging
function log(level, message, data = null) {
    const timestamp = new Date().toISOString();
    const logEntry = { timestamp, level, message, ...(data && { data }) };
    console.log(JSON.stringify(logEntry));
}

// Convert OpenAI messages to Claude format
function convertMessages(openaiMessages) {
    const claudeMessages = [];
    let systemPrompt = '';
    
    for (const msg of openaiMessages) {
        if (msg.role === 'system') {
            systemPrompt += msg.content + '\n';
        } else if (msg.role === 'user' || msg.role === 'assistant') {
            // Handle content that might be an array (vision/multimodal)
            let content = msg.content;
            if (Array.isArray(content)) {
                content = content.map(part => {
                    if (typeof part === 'string') return part;
                    if (part.type === 'text') return part.text;
                    return JSON.stringify(part);
                }).join('\n');
            }
            
            claudeMessages.push({
                role: msg.role,
                content: content
            });
        }
    }
    
    return { messages: claudeMessages, system: systemPrompt.trim() };
}

// Convert Claude response to OpenAI format
function convertResponse(claudeResponse, model) {
    const content = claudeResponse.content
        ?.map(block => block.type === 'text' ? block.text : '')
        ?.join('') || '';
    
    return {
        id: `chatcmpl-${Date.now()}`,
        object: 'chat.completion',
        created: Math.floor(Date.now() / 1000),
        model: model,
        choices: [{
            index: 0,
            message: {
                role: 'assistant',
                content: content
            },
            finish_reason: claudeResponse.stop_reason === 'end_turn' ? 'stop' : 
                          claudeResponse.stop_reason === 'max_tokens' ? 'length' : 
                          claudeResponse.stop_reason || 'stop'
        }],
        usage: {
            prompt_tokens: claudeResponse.usage?.input_tokens || 0,
            completion_tokens: claudeResponse.usage?.output_tokens || 0,
            total_tokens: (claudeResponse.usage?.input_tokens || 0) + 
                         (claudeResponse.usage?.output_tokens || 0)
        }
    };
}

// Convert Claude streaming chunk to OpenAI format
function convertStreamChunk(chunk, model) {
    if (chunk.type === 'content_block_delta') {
        return {
            id: `chatcmpl-${Date.now()}`,
            object: 'chat.completion.chunk',
            created: Math.floor(Date.now() / 1000),
            model: model,
            choices: [{
                index: 0,
                delta: {
                    content: chunk.delta?.text || ''
                },
                finish_reason: null
            }]
        };
    }
    
    if (chunk.type === 'message_stop') {
        return {
            id: `chatcmpl-${Date.now()}`,
            object: 'chat.completion.chunk',
            created: Math.floor(Date.now() / 1000),
            model: model,
            choices: [{
                index: 0,
                delta: {},
                finish_reason: 'stop'
            }]
        };
    }
    
    return null;
}

// Main handler - Chat Completions
app.post('/v1/chat/completions', async (req, res) => {
    const startTime = Date.now();
    
    try {
        const { model, messages, max_tokens, temperature, stream, top_p } = req.body;
        
        // Use configured model
        const claudeModel = DEFAULT_MODEL;
        
        // Convert to Claude format
        const { messages: claudeMessages, system } = convertMessages(messages);
        
        const claudeRequest = {
            model: claudeModel,
            messages: claudeMessages,
            max_tokens: max_tokens || 4096,
            ...(system && { system }),
            ...(temperature !== undefined && { temperature }),
            ...(top_p !== undefined && { top_p })
        };
        
        log('info', `Translating request`, { 
            from: model, 
            to: claudeModel,
            messageCount: claudeMessages.length,
            stream: !!stream 
        });
        
        if (stream) {
            // Streaming response
            res.setHeader('Content-Type', 'text/event-stream');
            res.setHeader('Cache-Control', 'no-cache');
            res.setHeader('Connection', 'keep-alive');
            res.setHeader('X-Accel-Buffering', 'no');
            
            const response = await axios.post(
                `${AZURE_ENDPOINT}/v1/messages`,
                { ...claudeRequest, stream: true },
                {
                    headers: {
                        'Content-Type': 'application/json',
                        'x-api-key': AZURE_API_KEY,
                        'anthropic-version': '2023-06-01'
                    },
                    responseType: 'stream',
                    timeout: 300000 // 5 minutes
                }
            );
            
            let buffer = '';
            
            response.data.on('data', (chunk) => {
                buffer += chunk.toString();
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';
                
                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6);
                        if (data === '[DONE]') {
                            res.write('data: [DONE]\n\n');
                            continue;
                        }
                        
                        try {
                            const claudeChunk = JSON.parse(data);
                            const openaiChunk = convertStreamChunk(claudeChunk, model);
                            if (openaiChunk) {
                                res.write(`data: ${JSON.stringify(openaiChunk)}\n\n`);
                            }
                        } catch (e) {
                            // Skip invalid JSON
                        }
                    }
                }
            });
            
            response.data.on('end', () => {
                res.write('data: [DONE]\n\n');
                res.end();
                log('info', `Stream completed`, { duration: Date.now() - startTime });
            });
            
            response.data.on('error', (err) => {
                log('error', `Stream error`, { error: err.message });
                res.end();
            });
            
        } else {
            // Non-streaming response
            const response = await axios.post(
                `${AZURE_ENDPOINT}/v1/messages`,
                claudeRequest,
                {
                    headers: {
                        'Content-Type': 'application/json',
                        'x-api-key': AZURE_API_KEY,
                        'anthropic-version': '2023-06-01'
                    },
                    timeout: 300000 // 5 minutes
                }
            );
            
            const openaiResponse = convertResponse(response.data, model);
            
            log('info', `Request completed`, { 
                duration: Date.now() - startTime,
                inputTokens: openaiResponse.usage.prompt_tokens,
                outputTokens: openaiResponse.usage.completion_tokens
            });
            
            res.json(openaiResponse);
        }
    } catch (error) {
        const errorData = error.response?.data || { message: error.message };
        log('error', `Request failed`, { 
            error: errorData,
            duration: Date.now() - startTime 
        });
        
        res.status(error.response?.status || 500).json({
            error: {
                message: errorData.error?.message || error.message,
                type: 'translator_error',
                code: error.response?.status || 500
            }
        });
    }
});

// Models endpoint
app.get('/v1/models', (req, res) => {
    res.json({
        object: 'list',
        data: [
            { 
                id: 'gpt-5', 
                object: 'model', 
                created: Math.floor(Date.now() / 1000),
                owned_by: 'azure-claude-translator',
                permission: [],
                root: 'gpt-5',
                parent: null
            },
            { 
                id: 'gpt-5.1', 
                object: 'model', 
                created: Math.floor(Date.now() / 1000),
                owned_by: 'azure-claude-translator',
                permission: [],
                root: 'gpt-5.1',
                parent: null
            }
        ]
    });
});

// Health check
app.get('/health', (req, res) => {
    res.json({ 
        status: 'ok',
        azure_endpoint: AZURE_ENDPOINT,
        default_model: DEFAULT_MODEL
    });
});

// Start server
const PORT = process.env.PORT || 8318;
app.listen(PORT, () => {
    log('info', `Translator started`, {
        port: PORT,
        azure_endpoint: AZURE_ENDPOINT,
        default_model: DEFAULT_MODEL
    });
    console.log(`\n🚀 OpenAI-to-Claude Translator running on port ${PORT}`);
    console.log(`   Azure endpoint: ${AZURE_ENDPOINT}`);
    console.log(`   Default model: ${DEFAULT_MODEL}\n`);
});
```

### Bước 4: Tạo file .env

```bash
# .env
AZURE_ENDPOINT=https://YOUR_RESOURCE.services.ai.azure.com/anthropic
AZURE_API_KEY=YOUR_AZURE_API_KEY
DEFAULT_MODEL=claude-opus-4-5
PORT=8318
```

### Bước 5: Cài đặt và chạy

```bash
# Cài đặt dependencies
npm install

# Chạy development mode
npm run dev

# Hoặc production mode
npm start

# Hoặc với PM2 (khuyến nghị)
npm install -g pm2
pm2 start index.js --name "openai-claude-translator"
pm2 save
```

### Bước 6: Cấu hình CLIProxyAPI

Cập nhật `config.yaml`:

```yaml
port: 8317
auth-dir: "~/.cli-proxy-api"
debug: true

# Amp integration
amp-upstream-url: "https://ampcode.com"
amp-restrict-management-to-localhost: true

# ============================================================================
# QUAN TRỌNG: Redirect Oracle GPT-5 → Claude Opus (Azure) via Translator
# ============================================================================
codex-api-key:
  - api-key: "dummy-key"  # Không cần API key thật
    base-url: "http://localhost:8318/v1"  # Translator Proxy

# Azure Claude config (cho các request Claude trực tiếp từ main agent)
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic"
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "YOUR_AZURE_API_KEY"
    models:
      - name: "claude-opus-4-5"
        alias: "claude-opus-4-5-20251101"
      - name: "claude-opus-4-5"
        alias: "claude-opus-4-5"
      - name: "claude-sonnet-4-5"
        alias: "claude-sonnet-4-5-20250929"
      - name: "claude-sonnet-4-5"
        alias: "claude-sonnet-4-5"
      - name: "claude-haiku-4-5"
        alias: "claude-haiku-4-5-20251001"
      - name: "claude-haiku-4-5"
        alias: "claude-haiku-4-5"
```

### Bước 7: Verify

```bash
# Test translator directly
curl http://localhost:8318/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "messages": [{"role": "user", "content": "Say hello"}],
    "max_tokens": 100
  }'

# Test through CLIProxyAPI
curl http://localhost:8317/api/provider/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "messages": [{"role": "user", "content": "Say hello"}],
    "max_tokens": 100
  }'
```

---

## Phương án 2: ccproxy-api (Go-based)

### Ưu điểm

- Performance cao hơn
- Binary đơn giản, không cần Node.js runtime

### Yêu cầu

- Go 1.21+
- CLIProxyAPI đang chạy

### Triển khai

```bash
cd /home/azureuser
git clone https://github.com/CaddyGlow/ccproxy-api.git
cd ccproxy-api

# Build
go build -o ccproxy-api ./cmd/main.go

# Cấu hình (cần fork và sửa để đổi hướng translation)
# Default: Claude → OpenAI
# Cần sửa thành: OpenAI → Claude
```

**Lưu ý**: ccproxy-api mặc định translate Claude → OpenAI. Để sử dụng cho use case này, cần fork và sửa code để đổi hướng translation.

---

## Tóm tắt luồng hoạt động

| Bước | Component | Action |
|------|-----------|--------|
| 1 | Amp CLI Oracle | Gửi request: `POST /api/provider/openai/v1/chat/completions` với `model: gpt-5` |
| 2 | CLIProxyAPI | Nhận request, tìm `codex-api-key` config, forward tới `localhost:8318` |
| 3 | Translator Proxy | Nhận OpenAI request, convert sang Claude format |
| 4 | Translator Proxy | Gửi tới Azure AI Foundry: `POST /v1/messages` với `model: claude-opus-4-5` |
| 5 | Azure AI Foundry | Xử lý và trả về Claude response |
| 6 | Translator Proxy | Convert Claude response → OpenAI format |
| 7 | CLIProxyAPI | Forward response về Amp CLI |
| 8 | Amp CLI Oracle | Nhận response (tưởng là từ GPT-5, thực tế là Claude Opus) |

### Sequence Diagram

```
┌─────────┐     ┌─────────────┐     ┌────────────┐     ┌──────────────┐
│ Amp CLI │     │ CLIProxyAPI │     │ Translator │     │ Azure Claude │
│ Oracle  │     │   :8317     │     │   :8318    │     │   Foundry    │
└────┬────┘     └──────┬──────┘     └─────┬──────┘     └──────┬───────┘
     │                 │                  │                   │
     │ POST /api/provider/openai/v1/chat/completions          │
     │ model: gpt-5    │                  │                   │
     │────────────────>│                  │                   │
     │                 │                  │                   │
     │                 │ POST /v1/chat/completions            │
     │                 │ model: gpt-5     │                   │
     │                 │─────────────────>│                   │
     │                 │                  │                   │
     │                 │                  │ POST /v1/messages │
     │                 │                  │ model: claude-opus│
     │                 │                  │──────────────────>│
     │                 │                  │                   │
     │                 │                  │   Claude Response │
     │                 │                  │<──────────────────│
     │                 │                  │                   │
     │                 │  OpenAI Response │                   │
     │                 │<─────────────────│                   │
     │                 │                  │                   │
     │  OpenAI Response│                  │                   │
     │<────────────────│                  │                   │
     │                 │                  │                   │
```

---

## Kết luận

### Không thể redirect trực tiếp

Vì OpenAI và Anthropic sử dụng **protocol khác nhau**, không thể đơn giản redirect mà cần **Translator Proxy**.

### Giải pháp khuyến nghị

1. **Phương án 1 (Node.js)**: Dễ triển khai, dễ customize, phù hợp cho hầu hết use cases
2. **Phương án 2 (Go)**: Nếu cần performance cao và đã quen với Go

### Checklist triển khai

- [ ] Tạo Translator Proxy (Node.js hoặc Go)
- [ ] Cấu hình Azure endpoint và API key
- [ ] Cập nhật `config.yaml` với `codex-api-key` trỏ tới Translator
- [ ] Test translator độc lập
- [ ] Test through CLIProxyAPI
- [ ] Verify Amp CLI Oracle sử dụng Claude response

---

## Tài liệu tham khảo

- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)
- [Azure AI Foundry Claude Documentation](https://learn.microsoft.com/en-us/azure/ai-services/openai/concepts/models)
- [CLIProxyAPI Documentation](https://help.router-for.me/)
- [Amp CLI Models](https://ampcode.com/models)
