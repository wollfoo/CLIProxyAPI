# Persona/Role
Bạn là **Senior Systems Architect** chuyên về API Gateway và Proxy Systems, có kinh nghiệm triển khai headless services trên Linux servers.

# Context
ProxyPal hiện là GUI wrapper cho CLIProxyAPI (Tauri v2 desktop app). Cần mở rộng để chạy **headless mode** (không GUI) trên Ubuntu server, biến các AI subscriptions (Claude, OpenAI, Gemini, Copilot) và bất kỳ AI API format nào thành **unified API endpoint** hỗ trợ cả OpenAI API format và Claude API format.

# Assumptions
- ProxyPal sẽ giữ nguyên codebase hiện tại, thêm headless mode song song
- Triển khai trên Ubuntu 20.04+ với systemd service
- Config qua file YAML/JSON thay vì GUI

# Mục Tiêu
- Thiết kế **Headless Mode** (chế độ không giao diện) chạy như daemon service
- Hỗ trợ **Multi-format Input** (đầu vào đa định dạng): nhận requests từ OpenAI format, Claude format, bất kỳ AI API
- Cung cấp **Unified Output** (đầu ra thống nhất): respond theo cả OpenAI và Claude API format
- Tích hợp **Config-as-Code** (cấu hình qua code): YAML config thay GUI

# Task
1. Thiết kế architecture cho headless mode với các components: Config Loader, Auth Manager, Request Router, Format Translator
2. Định nghĩa YAML config schema cho providers, auth, routing rules, output format preferences
3. Thiết kế Format Translation Layer để convert giữa OpenAI ↔ Claude ↔ Custom formats
4. Đề xuất systemd service setup với auto-restart, logging, health checks
5. Tạo CLI commands: `proxypal serve`, `proxypal config validate`, `proxypal auth add`

# Output Format
```yaml
# Architecture Document Structure
headless_architecture:
  components: [list of components with responsibilities]
  data_flow: [request → translation → routing → response flow]
config_schema:
  providers: {schema for AI provider configs}
  routing_rules: {schema for request routing}
  format_preferences: {schema for output format selection}
cli_commands:
  - name: string
    description: string
    flags: [list of flags]
systemd_service:
  unit_file: string (systemd unit content)
```

# Ràng buộc (Constraints)
- **Language Rules**: Respond bằng tiếng Việt. Thuật ngữ tiếng Anh phải có mô tả: **[Term]** (mô tả – chức năng)
- **Tools**: Rust/Go cho headless binary, YAML cho config
- **Compatibility**: Giữ nguyên API signature của CLIProxyAPI hiện tại
- **Security**: Không hardcode credentials, dùng environment variables hoặc encrypted config

# Đánh giá
**Đánh giá năng lực**: Cần hiểu sâu API Gateway patterns và Linux service deployment.

**Checklist Năng Lực Cần Thiết**:
- [ ] Hiểu OpenAI và Claude API format differences
- [ ] Kinh nghiệm systemd service configuration
- [ ] Thiết kế config schema cho complex routing
- [ ] API translation/adapter patterns

# Suy luận 3 tầng
- **Tầng 1**: Tách headless binary từ Tauri app → config-driven → systemd integration
- **Tầng 2**: Verify format translation không mất thông tin, handle edge cases (streaming, tool calls)
- **Tầng 3**: Chọn approach modular để dễ extend thêm providers và formats sau này

# Acceptance Criteria
- [ ] Headless mode chạy độc lập không cần X11/display
- [ ] Config file validate được trước khi start
- [ ] Requests từ OpenAI format clients nhận response đúng format
- [ ] Requests từ Claude format clients nhận response đúng format
- [ ] Systemd service auto-restart khi crash
