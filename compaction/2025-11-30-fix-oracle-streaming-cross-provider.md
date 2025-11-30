# Session Compaction: Fix Oracle Streaming Cross-Provider Routing

**Date**: 2025-11-30  
**Duration**: ~2 hours  
**Status**: ✅ Completed Successfully

---

## Objective

Fix bugs in cross-provider routing from **Oracle GPT-5.1** to **Azure Claude Opus 4.5**, ensuring the OpenAI Responses API streaming format is fully compatible with the Amp CLI client.

---

## Root Causes Identified

1. **Missing `output:[]` in initial events** - `response.created` and `response.in_progress` events were missing required fields, causing client to close connection
2. **Missing `output_index`** - Text message events had hardcoded `output_index: 0` instead of using actual index from Claude
3. **Missing text content in done events** - `response.output_text.done` and related events didn't include accumulated text
4. **Missing `name` field in function call events** - `response.function_call_arguments.done` and `response.output_item.done` for function calls were missing the function name

---

## Files Modified

### `/internal/translator/claude/openai/responses/claude_openai-responses_response.go`

#### 1. Fix `response.created` event (lines ~107-111)
```go
// BEFORE
created := `{"type":"response.created","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"in_progress","model":""}}`

// AFTER - Added output:[] and all required fields
created := `{"type":"response.created","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"in_progress","background":false,"error":null,"incomplete_details":null,"instructions":null,"metadata":{},"model":"","output":[],"parallel_tool_calls":true,"temperature":1,"tool_choice":"auto","top_p":1,"max_output_tokens":null,"previous_response_id":null,"reasoning":null,"service_tier":"default","store":true,"text":{"format":{"type":"text"}},"truncation":"disabled","usage":null}}`
```

#### 2. Fix `response.in_progress` event (lines ~114-119)
```go
// BEFORE
inprog := `{"type":"response.in_progress","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"in_progress","model":""}}`

// AFTER - Added output:[]
inprog := `{"type":"response.in_progress","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"in_progress","model":"","output":[]}}`
```

#### 3. Fix `output_index` for text message events (lines ~137-147)
```go
// Added output_index to response.output_item.added
item, _ = sjson.Set(item, "output_index", idx)

// Added output_index to response.content_part.added
part, _ = sjson.Set(part, "output_index", idx)
```

#### 4. Fix `output_index` for text delta events (lines ~190-197)
```go
// Added idx extraction and output_index setting
idx := int(root.Get("index").Int())
msg, _ = sjson.Set(msg, "output_index", idx)
```

#### 5. Fix text done events with content (lines ~236-253)
```go
// Added output_index and text content to all done events
done, _ = sjson.Set(done, "output_index", idx)
done, _ = sjson.Set(done, "text", st.TextBuf.String())

partDone, _ = sjson.Set(partDone, "output_index", idx)
partDone, _ = sjson.Set(partDone, "part.text", st.TextBuf.String())

final, _ = sjson.Set(final, "output_index", idx)
final, _ = sjson.Set(final, "item.content.0.text", st.TextBuf.String())
```

#### 6. Fix function call done events with name (lines ~262-278)
```go
// Get function name from stored metadata
funcName := st.FuncNames[idx]

// Added name to response.function_call_arguments.done
fcDone := `{"type":"response.function_call_arguments.done","sequence_number":0,"item_id":"","output_index":0,"arguments":"","name":""}`
fcDone, _ = sjson.Set(fcDone, "name", funcName)

// Added name to response.output_item.done
itemDone, _ = sjson.Set(itemDone, "item.name", funcName)
```

---

## Test Results

### Before Fix
```
[ERROR] scanner error: context canceled
```
Client (Amp CLI) closed connection prematurely due to malformed SSE events.

### After Fix
```
[2025-11-30 12:23:54] cross-provider executor: stream completed normally
[2025-11-30 12:23:54] [GIN] 200 | 1m23s | POST "/api/provider/openai/v1/responses"
```
Stream completed successfully with all 1462 sequence events delivered.

---

## Summary of Changes

| Event Type | Field Added | Purpose |
|------------|-------------|---------|
| `response.created` | `output:[]`, metadata fields | Required by OpenAI Responses API spec |
| `response.in_progress` | `output:[]` | Required by OpenAI Responses API spec |
| `response.output_item.added` | `output_index` | Correct block index from Claude |
| `response.content_part.added` | `output_index` | Correct block index from Claude |
| `response.output_text.delta` | `output_index` | Correct block index from Claude |
| `response.output_text.done` | `output_index`, `text` | Full text content |
| `response.content_part.done` | `output_index`, `part.text` | Full text content |
| `response.output_item.done` | `output_index`, `item.content.0.text` | Full text content |
| `response.function_call_arguments.done` | `name` | Function name for tool calls |
| `response.output_item.done` (func) | `item.name` | Function name for tool calls |

---

## Verification Commands

```bash
# Build
cd /home/azureuser/cliproxyapi/CLIProxyAPI
go build -o cli-proxy-api ./cmd/server

# Run with Oracle-to-Claude config
./cli-proxy-api --config ./config.oracle-to-azure-claude.yaml

# Watch for successful streaming
# Should see "stream completed normally" instead of "context canceled"
```

---

## Related Files (Reference)

- `/internal/runtime/executor/cross_provider_executor.go` - Streaming executor
- `/internal/translator/claude/openai/responses/claude_openai-responses_request.go` - Request translator
- `/sdk/api/handlers/openai/openai_responses_handlers.go` - HTTP handler

---

## Lessons Learned

1. OpenAI Responses API requires `output:[]` in initial events even when empty
2. All streaming events must have consistent `output_index` matching the content block index from Claude
3. Done events should include the full accumulated content, not empty strings
4. Function call events require the `name` field for proper tool identification
