# spec: observability
```
service: personal-ai-agent
version: 1.0
file:    observability
```
---
## Logging
| Property        | Value                          |
|-----------------|--------------------------------|
| Format          | structured JSON                |
| Destination     | CloudWatch Logs                |
| Required fields | `correlation_id`, `request_id` |
| Log on success  | `ask.invoked`                  |
| Log on failure  | `ask.rejected`                 |
> Infrastructure logging must not include request or response bodies.

## Response Tracing
| Property | Value                                                       |
|----------|-------------------------------------------------------------|
| Header   | `X-Correlation-Id`                                          |
| Rule     | Every HTTP response returns the correlation ID used in logs |

### Event: `ask.invoked`
Emitted after a successful response is returned to the caller.
```json
{
  "event":          "ask.invoked",
  "correlation_id": "<uuid>",
  "request_id":     "<lambda-request-id>",
  "conversation_id": "<uuid>",
  "latency_ms":     142,
  "model":          "gpt-4o"
}
```

### Event: `ask.rejected`
Emitted when validation fails **or** an upstream/internal error prevents a response.
```json
{
  "event":          "ask.rejected",
  "correlation_id": "<uuid>",
  "request_id":     "<lambda-request-id>",
  "reason":         "INVALID_QUESTION | INVALID_INPUT | RATE_LIMITED | UPSTREAM_ERROR | INTERNAL_ERROR",
  "http_status":    400
}
```
> `question` content is **never** written to logs (see S-03). API key and system prompt are **never** written to logs (see S-01, S-02).
> For `reason="openai_malformed_response"`, logs may include a bounded, sanitized preview of model output for debugging (whitespace-normalized and truncated to a short fixed limit).
---
## Metrics
| Metric                 | When emitted                         | Unit         | Destination  |
|------------------------|--------------------------------------|--------------|--------------|
| `ask.request.count`    | Every invocation                     | Count        | CloudWatch   |
| `ask.request.latency`  | Every invocation                     | Milliseconds | CloudWatch   |
| `ask.request.rejected` | Validation failure or upstream error | Count        | CloudWatch   |
