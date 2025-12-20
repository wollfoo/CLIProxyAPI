<role>
Bạn là **AI Gateway Architect** (kiến trúc sư cổng AI – chuyên thiết kế proxy/router cho LLM APIs).
</role>

<context>
Người dùng có nhiều **subscription** (gói đăng ký – tài khoản trả phí) và **API keys** từ các nhà cung cấp AI. Mục tiêu: Hợp nhất tất cả thành **một proxy endpoint duy nhất** tương thích chuẩn **OpenAI API format** và **Anthropic API format**.

<providers_inventory>
<!-- Tier 1: Major Cloud Providers -->
<provider name="OpenAI" format="openai" models="gpt-4o, gpt-4-turbo, o1, o3">
  <auth type="api_key" env="OPENAI_API_KEY"/>
  <base_url>https://api.openai.com/v1</base_url>
</provider>

<provider name="Anthropic" format="anthropic" models="claude-3.5-sonnet, claude-3-opus, claude-4">
  <auth type="api_key" env="ANTHROPIC_API_KEY"/>
  <base_url>https://api.anthropic.com/v1</base_url>
</provider>

<provider name="Google AI Studio" format="gemini" models="gemini-2.0-flash, gemini-pro">
  <auth type="api_key" env="GOOGLE_AI_API_KEY"/>
  <base_url>https://generativelanguage.googleapis.com/v1beta</base_url>
</provider>

<provider name="Azure OpenAI" format="openai" models="gpt-4, gpt-35-turbo">
  <auth type="api_key" env="AZURE_OPENAI_API_KEY"/>
  <base_url>https://{resource}.openai.azure.com/openai/deployments/{deployment}</base_url>
  <extra_headers>api-version: 2024-02-15-preview</extra_headers>
</provider>

<provider name="Azure AI Foundry" format="azure_ai" models="phi-3, llama-3, mistral-large">
  <auth type="api_key" env="AZURE_AI_FOUNDRY_API_KEY"/>
  <base_url>https://{endpoint}.inference.ai.azure.com</base_url>
</provider>

<provider name="AWS Bedrock" format="bedrock" models="anthropic.claude-3, meta.llama3, mistral.mixtral">
  <auth type="aws_credentials" env="AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY"/>
  <region>us-east-1</region>
</provider>

<provider name="Google Vertex AI" format="vertex" models="gemini-pro, claude-3-sonnet">
  <auth type="service_account" env="GOOGLE_APPLICATION_CREDENTIALS"/>
  <project>your-gcp-project</project>
</provider>

<!-- Tier 2: Specialized AI Providers -->
<provider name="Mistral AI" format="openai" models="mistral-large, mixtral-8x22b, codestral">
  <auth type="api_key" env="MISTRAL_API_KEY"/>
  <base_url>https://api.mistral.ai/v1</base_url>
</provider>

<provider name="Cohere" format="cohere" models="command-r-plus, embed-english-v3">
  <auth type="api_key" env="COHERE_API_KEY"/>
  <base_url>https://api.cohere.ai/v1</base_url>
</provider>

<provider name="Perplexity" format="openai" models="llama-3.1-sonar-large, pplx-70b-online">
  <auth type="api_key" env="PERPLEXITY_API_KEY"/>
  <base_url>https://api.perplexity.ai</base_url>
</provider>

<provider name="Groq" format="openai" models="llama-3.2-90b, mixtral-8x7b">
  <auth type="api_key" env="GROQ_API_KEY"/>
  <base_url>https://api.groq.com/openai/v1</base_url>
</provider>

<provider name="Together AI" format="openai" models="meta-llama/Llama-3-70b, mistralai/Mixtral-8x22B">
  <auth type="api_key" env="TOGETHER_API_KEY"/>
  <base_url>https://api.together.xyz/v1</base_url>
</provider>

<provider name="Fireworks AI" format="openai" models="accounts/fireworks/models/llama-v3p1-70b">
  <auth type="api_key" env="FIREWORKS_API_KEY"/>
  <base_url>https://api.fireworks.ai/inference/v1</base_url>
</provider>

<provider name="DeepSeek" format="openai" models="deepseek-chat, deepseek-coder">
  <auth type="api_key" env="DEEPSEEK_API_KEY"/>
  <base_url>https://api.deepseek.com/v1</base_url>
</provider>

<!-- Tier 3: Code-focused & Agentic -->
<provider name="GitHub Copilot" format="copilot" models="gpt-4o, claude-3.5-sonnet">
  <auth type="oauth" env="GITHUB_TOKEN"/>
  <base_url>https://api.githubcopilot.com</base_url>
</provider>

<provider name="Antigravity" format="openai" models="antigravity-agent">
  <auth type="api_key" env="ANTIGRAVITY_API_KEY"/>
</provider>

<!-- Tier 4: Open-source Hosting -->
<provider name="Hugging Face" format="openai" models="meta-llama/Llama-3.2-3B, mistralai/Mistral-7B">
  <auth type="api_key" env="HF_API_KEY"/>
  <base_url>https://api-inference.huggingface.co/models</base_url>
</provider>

<provider name="Replicate" format="replicate" models="meta/llama-2-70b-chat">
  <auth type="api_key" env="REPLICATE_API_TOKEN"/>
  <base_url>https://api.replicate.com/v1</base_url>
</provider>

<provider name="Deepinfra" format="openai" models="meta-llama/Llama-3-70b-Instruct">
  <auth type="api_key" env="DEEPINFRA_API_KEY"/>
  <base_url>https://api.deepinfra.com/v1/openai</base_url>
</provider>
</providers_inventory>

<technical_requirements>
- **Endpoint đích**: `http://localhost:8317/v1`
- **Tương thích**: OpenAI `/v1/chat/completions` + Anthropic `/v1/messages`
- **Stack hiện tại**: Go (xem file `config.yaml` trong workspace)
</technical_requirements>
</context>

<task>
Thiết kế và triển khai **AI Gateway Proxy** với các chức năng:

1. **Unified Routing** (định tuyến hợp nhất – route request đến đúng provider)
   - Chuyển đổi format request/response giữa các chuẩn API (OpenAI, Anthropic, Gemini, Cohere, Bedrock, Vertex)
   - Hỗ trợ model aliasing (ví dụ: `gpt-4` → OpenAI, `claude-3` → Anthropic, `gemini-pro` → Google)
   - Nhận diện provider từ model name hoặc explicit routing header

2. **Multi-Provider Load Balancing** (cân bằng tải đa provider)
   - Round-robin hoặc weighted routing trong cùng model category
   - Fallback chain: Primary → Secondary → Tertiary provider
   - Health check cho từng provider endpoint
   - Circuit breaker khi provider liên tục fail

3. **Authentication Management** (quản lý xác thực)
   - Một API key duy nhất cho proxy (`X-Proxy-Key`)
   - Backend quản lý nhiều loại auth:
     - API Key (OpenAI, Anthropic, Mistral, Groq...)
     - Azure AD / Service Account (Azure, GCP)
     - AWS Credentials (Bedrock)
     - OAuth tokens (GitHub Copilot)

4. **Rate Limiting & Quota Management** (giới hạn tốc độ & quản lý hạn mức)
   - Theo dõi usage của từng provider (TPM, RPM, tokens)
   - Tự động chuyển provider khi hết quota hoặc rate limited
   - Budget alerts và spending caps

5. **Format Conversion Layer** (tầng chuyển đổi định dạng)
   - OpenAI → Anthropic và ngược lại
   - OpenAI → Gemini và ngược lại
   - OpenAI → Cohere và ngược lại
   - OpenAI → Bedrock và ngược lại
   - Streaming support cho tất cả providers
</task>

<constraints>
- KHÔNG thay đổi cấu trúc thư mục hiện tại nếu không cần thiết
- Ưu tiên sử dụng code Go có sẵn trong workspace
- Config phải dễ mở rộng khi thêm provider mới (chỉ cần thêm vào YAML)
- Logging đầy đủ cho debugging với request tracing
- Error messages phải rõ ràng, không expose internal API keys
- Sensitive data (API keys) phải load từ environment variables
- Support cả sync và streaming responses
</constraints>

<output_format>
Trả về theo cấu trúc:

## 1. Architecture Overview
- Sơ đồ luồng request (dùng Mermaid)
- Giải thích các component chính
- Provider adapter pattern

## 2. Configuration Schema
- Cấu trúc file `config.yaml` với TẤT CẢ providers đã liệt kê
- Environment variables mapping
- Model aliasing configuration

## 3. Implementation Plan
- Danh sách files cần tạo/sửa
- Thứ tự triển khai (adapters → router → proxy)

## 4. Code Changes
- Provider adapters (OpenAI, Anthropic, Gemini, Azure, Bedrock, Cohere, etc.)
- Request/Response converters
- Unit tests cho critical paths
</output_format>

<acceptance_criteria>
- [ ] Request đến `POST /v1/chat/completions` được route đúng provider
- [ ] Request đến `POST /v1/messages` được xử lý đúng format Anthropic
- [ ] Azure OpenAI và Azure AI Foundry hoạt động với đúng auth headers
- [ ] AWS Bedrock requests được sign đúng với AWS credentials
- [ ] Fallback hoạt động khi provider chính timeout/error
- [ ] Config thêm provider mới không cần thay đổi code
- [ ] Logs cho phép trace request từ đầu đến cuối
- [ ] Streaming responses hoạt động cho tất cả providers
</acceptance_criteria>