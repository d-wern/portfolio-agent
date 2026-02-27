# spec: interface — POST /ask
```
service: personal-ai-agent
version: 1.0
file:    interfaces/post-ask
```
---
## Endpoint
| Property     | Value            |
|--------------|------------------|
| Method       | POST             |
| Path         | `/ask`           |
| Auth         | none             |
| CORS         | true             |
| Content-Type | application/json |
---
## Request
| Field            | Type   | Required | Constraints                                                  |
|------------------|--------|----------|--------------------------------------------------------------|
| `question`       | string | ✅        | non-empty, maxLength: 300                                    |
| `conversationId` | string | ❌        | if omitted, a UUID is generated and returned in the response |
```json
{
  "question": "What technologies do you specialise in?",
  "conversationId": "conv-abc"
}
```
---
## Response
### Headers (all responses)
| Header             | Value                                                        |
|--------------------|--------------------------------------------------------------|
| `Content-Type`     | `application/json`                                           |
| `X-Correlation-Id` | request correlation ID (client-supplied or generated UUID)   |
> Request header matching for `X-Correlation-Id` is case-insensitive.

### `200 OK`
```json
{
  "answer": "<string>",
  "conversationId": "<string>"
}
```
### `400 Bad Request`
```json
{ "error": "INVALID_INPUT" }
```
```json
{ "error": "INVALID_QUESTION" }
```
### `429 Too Many Requests`
```json
{ "error": "RATE_LIMITED" }
```
### `502 Bad Gateway`
```json
{ "error": "UPSTREAM_ERROR" }
```
### `500 Internal Server Error`
```json
{ "error": "INTERNAL_ERROR" }
```
---
## Validation Rules
| Field            | Rule                                                                                                                                                                                                                                                                                              | Error Code         |
|------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------|
| `question`       | Must be non-empty                                                                                                                                                                                                                                                                                 | `INVALID_INPUT`    |
| `question`       | Length ≤ 300 characters                                                                                                                                                                                                                                                                           | `INVALID_INPUT`    |
| `conversationId` | Existing conversations may contain at most 10 successful in-scope user turns; requests beyond that limit are rejected                                                                                                                                                                             | `INVALID_INPUT`    |
| `question`       | Must be relevant to recruiting for a professional role. Relevance and final answer are produced in a single OpenAI Chat Completions call with structured output; questions unrelated to professional background, skills, projects, experience, or role fit are rejected before any database write | `INVALID_QUESTION` |
| `question`       | Unsafe content is rejected via the **OpenAI Moderation API** (`/v1/moderations`)                                                                                                                                                                                                                  | `INVALID_QUESTION` |
> No database write occurs when validation fails.
> For successful in-scope requests, the final message record and conversation metadata are persisted together in one atomic write; the service does not persist an intermediate pending record.

## LLM Structured Output Contract
For the combined relevance+answer call, the service requests OpenAI Chat Completions with `response_format` set to a strict JSON schema that requires:
- `in_scope` (`boolean`)
- `answer` (`string`)

The service must parse the model output as this JSON object directly and reject any unknown fields.
If parsing fails, or `in_scope=true` with an empty `answer`, the request is rejected as `502 UPSTREAM_ERROR`.

## Error Code Reference
| HTTP Status | Error Code         | Cause                                                                                                |
|-------------|--------------------|------------------------------------------------------------------------------------------------------|
| `400`       | `INVALID_INPUT`    | Missing or oversized `question` field                                                                |
| `400`       | `INVALID_QUESTION` | Off-topic or unsafe question                                                                         |
| `429`       | `RATE_LIMITED`     | OpenAI returned `429` (moderation or combined relevance+answer generation call)                      |
| `500`       | `INTERNAL_ERROR`   | SSM or DynamoDB failure                                                                              |
| `502`       | `UPSTREAM_ERROR`   | OpenAI returned `5xx` or malformed payload (moderation or combined relevance+answer generation call) |
---
## Examples
### ✅ Valid question — with existing history
**Given:** `conversationId: "conv-abc"`, existing history, `question: "What technologies do you specialise in?"`
**Expected:** `200` — `{ "answer": "<string>", "conversationId": "conv-abc" }`
### ✅ Valid question — no conversationId
**Given:** no `conversationId`, `question: "What is your background?"`
**Expected:** `200` — `{ "answer": "<string>", "conversationId": "<generated-uuid>" }`
**And:** response header `X-Correlation-Id` is present
### ❌ Missing question field
**Given:** body with no `question` field
**Expected:** `400` — `{ "error": "INVALID_INPUT" }`
### ❌ Empty question
**Given:** `question: ""`
**Expected:** `400` — `{ "error": "INVALID_INPUT" }`
### ❌ Question too long
**Given:** `question` of length 301
**Expected:** `400` — `{ "error": "INVALID_INPUT" }`
### ❌ Conversation exceeds max turns
**Given:** `conversationId` already has 10 successful in-scope user turns
**Expected:** `400` — `{ "error": "INVALID_INPUT" }`
### ❌ Question contains unsafe content
**Given:** `question` containing profanity or other unsafe content
**Expected:** `400` — `{ "error": "INVALID_QUESTION" }`
### ❌ Question is off-topic (politics)
**Given:** `question: "What do you think about the current election?"`
**Expected:** `400` — `{ "error": "INVALID_QUESTION" }`
### ❌ Question is off-topic (non-recruiting preference)
**Given:** `question: "What is your favorite movie genre?"`
**Expected:** `400` — `{ "error": "INVALID_QUESTION" }`
### ✅ Question is in-scope (sports in professional context)
**Given:** `question: "Did team sports influence your leadership style at work?"`
**Expected:** `200` — `{ "answer": "<string>", "conversationId": "<string>" }`
### ✅ Question is in-scope (weather/location in professional context)
**Given:** `question: "How do you handle commuting or remote work during bad weather?"`
**Expected:** `200` — `{ "answer": "<string>", "conversationId": "<string>" }`
### ❌ Relevance classifier rate limited
**Given:** valid question, combined relevance+answer upstream call returns `429`
**Expected:** `429` — `{ "error": "RATE_LIMITED" }`
**And:** response header `X-Correlation-Id` is present
### ❌ Relevance classifier unavailable
**Given:** valid question, combined relevance+answer upstream call returns `5xx` or malformed structured payload
**Expected:** `502` — `{ "error": "UPSTREAM_ERROR" }`
**And:** response header `X-Correlation-Id` is present
### ❌ Relevance payload missing required answer
**Given:** valid question, combined relevance+answer response sets `in_scope=true` with empty `answer`
**Expected:** `502` — `{ "error": "UPSTREAM_ERROR" }`
### ❌ OpenAI rate limited
**Given:** valid question, OpenAI returns `429`
**Expected:** `429` — `{ "error": "RATE_LIMITED" }`
### ❌ OpenAI unavailable
**Given:** valid question, OpenAI returns `5xx`
**Expected:** `502` — `{ "error": "UPSTREAM_ERROR" }`
**And:** response header `X-Correlation-Id` is present
