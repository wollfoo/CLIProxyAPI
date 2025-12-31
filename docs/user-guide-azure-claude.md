# Hướng dẫn sử dụng: CLIProxyAPI với Azure Claude cho Amp CLI

Tài liệu này hướng dẫn cách cài đặt và sử dụng phiên bản CLIProxyAPI đã được tùy chỉnh để hỗ trợ **Azure AI Foundry (Claude)** cho Amp CLI.

Phiên bản này giải quyết vấn đề Amp CLI luôn fallback về `ampcode.com` khi không có OAuth token, bằng cách cho phép định tuyến trực tiếp đến Azure Claude thông qua cấu hình API Key.

---

## 1. Yêu cầu chuẩn bị

### 1.1 Phần mềm
- **Amp CLI**: Đã cài đặt (phiên bản mới nhất).
- **Git**: Để clone mã nguồn.
- **Go**: Phiên bản 1.21 trở lên (để build từ source).

### 1.2 Tài khoản Azure
Bạn cần có quyền truy cập vào **Azure AI Foundry** với một model Claude đã được deploy (ví dụ: `claude-sonnet-4-5`).
- **Endpoint URL**: Dạng `https://<resource-name>.services.ai.azure.com/anthropic`
- **API Key**: Key quản lý resource này.

---

## 2. Cài đặt CLIProxyAPI

### 2.1 Clone mã nguồn

Clone từ repository fork của bạn:

```bash
git clone -b feature/azure-claude-provider https://github.com/wollfoo/CLIProxyAPI.git
cd CLIProxyAPI
```

### 2.2 Build ứng dụng

```bash
go build -o cli-proxy-api ./cmd/server

```

Sau khi build thành công, bạn sẽ có file thực thi `cli-proxy-api` trong thư mục hiện tại.

---

## 3. Cấu hình

Đây là bước quan trọng nhất. Bạn cần tạo file `config.yaml` để khai báo thông tin Azure Claude và mapping model.

### 3.1 Tạo file config.yaml

Tạo file `config.yaml` (có thể copy từ `config.example.yaml`) và thêm phần cấu hình sau:

```yaml
# Server port
port: 8317

# Amp integration
amp-upstream-url: "https://ampcode.com"
amp-restrict-management-to-localhost: true

# Authentication directory
auth-dir: "~/.cli-proxy-api"

# [QUAN TRỌNG] Cấu hình Azure Claude
claude-api-key:
  - api-key: "YOUR_AZURE_API_KEY"  # Thay bằng API Key của bạn
    base-url: "https://YOUR_RESOURCE.services.ai.azure.com/anthropic" # Endpoint Azure
    headers:
      anthropic-version: "2023-06-01"
      x-api-key: "YOUR_AZURE_API_KEY" # Cần lặp lại API Key ở đây
    models:
      # Mapping tên model từ Amp CLI sang tên model trên Azure
      # Format: 
      #   name:  "Tên model trên Azure Foundry"
      #   alias: "Tên model mà Amp CLI yêu cầu"
      
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
```

### 3.2 Lưu ý về Mapping Model

Amp CLI thường yêu cầu các model name cụ thể kèm ngày tháng (ví dụ `claude-sonnet-4-5-20250929`). Azure Foundry thường có tên ngắn gọn hơn (ví dụ `claude-sonnet-4-5`).

Bạn cần đảm bảo `models[].name` khớp chính xác với **Deployment Name** trên Azure AI Foundry Portal của bạn.

---

## 4. Chạy Proxy

Mở terminal và chạy lệnh:

```bash
./cli-proxy-api --config config.yaml
```

Nếu thành công, bạn sẽ thấy log:
```
INFO [...] Starting API server on :8317
INFO [...] Amp upstream proxy enabled for: https://ampcode.com
```

---

## 5. Cấu hình Amp CLI

Bạn cần trỏ Amp CLI về proxy server local thay vì server mặc định.

Mở file cấu hình của Amp CLI (thường tại `~/.config/amp/settings.json`) và thêm/sửa:

```json
{
  "amp.url": "http://localhost:8317"
}
```

Hoặc set biến môi trường (tùy phiên bản Amp):
```bash
export AMP_URL="http://localhost:8317"
```

---

## 6. Kiểm tra hoạt động

### 6.1 Kiểm tra log Proxy

Khi bạn chạy lệnh Amp (ví dụ `amp chat "Hello"`), hãy quan sát log của `cli-proxy-api`.

**Thành công (Dùng Azure):**
```
INFO [...] amp fallback: model claude-sonnet-4-5-20250929 matched claude-api-key alias, using local provider
```
Điều này có nghĩa là proxy đã nhận diện model và chuyển hướng sang Azure Claude executor.

**Thất bại (Fallback sang Ampcode):**
```
INFO [...] amp fallback: model ... has no configured provider, forwarding to ampcode.com
```
Nếu thấy dòng này, kiểm tra lại phần `models` trong `config.yaml` xem alias có khớp với model mà Amp CLI đang gọi không.

### 6.2 Lỗi thường gặp

1. **401 Unauthorized / Missing API Key**:
   - Kiểm tra `x-api-key` trong phần `headers` của `config.yaml`. Azure Foundry yêu cầu header này.

2. **DeploymentNotFound**:
   - Tên model trong `models[].name` không khớp với deployment trên Azure. Kiểm tra lại Azure Portal.

3. **Connection Refused**:
   - Proxy chưa chạy hoặc port 8317 bị chặn/bận.

---

## 7. Nâng cao: Debugging

Nếu gặp vấn đề, bạn có thể bật chế độ debug trong `config.yaml`:

```yaml
debug: true
```

Log sẽ hiện chi tiết request/response body, giúp bạn xác định chính xác model name mà Amp CLI đang gửi đi để config mapping cho đúng.
