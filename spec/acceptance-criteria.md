# spec: acceptance-criteria
```
service: personal-ai-agent
version: 1.0
file:    acceptance-criteria
```
Criteria are grouped by concern. Each criterion is a testable statement of what the system **must** guarantee — not how it achieves it.
---
## Conversation Behaviour
| ID   | Criterion                                                                                                                                                               |
|------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| B-01 | The system instructs the model to answer in first person as the portfolio owner, using a professional and concise tone                                                  |
| B-02 | The system instructs the model to answer the current request `question`, not a previous question in the conversation                                                    |
| B-03 | Follow-up questions use relevant completed conversation history when available                                                                                          |
| B-04 | The system instructs the model to reply with `"I don't have that information."` when the required information is unavailable in resume, interests, or completed history |
| B-05 | Questions classified by the LLM as unrelated to recruiting for a professional role are rejected with `400 INVALID_QUESTION`                                             |
| B-06 | Off-topic detection and final answer generation are produced in one LLM call with structured output (not static keyword matching)                                       |
| B-07 | Combined relevance+answer generation requests OpenAI with strict JSON schema output containing `in_scope` and `answer`                                                  |
---
## Context Bounds
| ID   | Criterion                                                                                                                   |
|------|-----------------------------------------------------------------------------------------------------------------------------|
| C-01 | History retrieval is limited to `MAX_CONTEXT_ITEMS` records                                                                 |
| C-02 | History retrieval favors the most recent completed turns when history exceeds limits                                        |
| C-03 | Only completed prior turns are included in prompt context                                                                   |
| C-04 | A completed prior turn is represented as one stored user-turn record containing both the user question and assistant answer |
| C-05 | Pending or incomplete prior turns are excluded from prompt context                                                          |
| C-06 | Final prompt history order remains chronological (oldest to newest)                                                         |
---
## Write Ordering & State
| ID   | Criterion                                                                                                                                                                       |
|------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| W-01 | For in-scope requests, the question record is persisted only after a successful combined relevance+answer LLM response                                                          |
| W-02 | The persisted question record includes the final `answer` when it is written                                                                                                    |
| W-03 | If the combined relevance+answer LLM call fails (or returns out-of-scope), no question record is written                                                                        |
| W-04 | The successful message record and conversation metadata update are committed atomically so turn counts cannot drift behind persisted message history                            |
| W-05 | Conversation metadata `turns` reflects the total number of successful in-scope user turns in the conversation, not just the number of history records loaded for prompt context |
| W-06 | A conversation accepts at most 10 successful in-scope user turns; the 11th request for the same `conversationId` is rejected with `400 INVALID_INPUT`                           |
---
## Error Mapping
| ID   | Criterion                                                                                                               |
|------|-------------------------------------------------------------------------------------------------------------------------|
| E-01 | An OpenAI `429` response results in a `429 RATE_LIMITED` response to the client                                         |
| E-02 | OpenAI failures other than rate limiting result in a `502 UPSTREAM_ERROR` response to the client                        |
| E-03 | An SSM or DynamoDB failure results in a `500 INTERNAL_ERROR` response to the client                                     |
| E-04 | OpenAI status classification is based on upstream HTTP status code, not string matching                                 |
| E-05 | Malformed structured payloads from the combined relevance+answer call result in `502 UPSTREAM_ERROR`                    |
| E-06 | Structured output parsing is strict JSON; malformed payloads and unknown fields are rejected without wrapper extraction |
| E-07 | Every response includes the correlation ID in an `X-Correlation-Id` header so clients can trace logs                    |
| E-08 | Correlation ID input accepts `X-Correlation-Id` case-insensitively and reuses the provided value                        |
---
## Security
| ID   | Criterion                                                                |
|------|--------------------------------------------------------------------------|
| S-01 | The OpenAI API key is never logged or included in any response body      |
| S-02 | Internal system prompt content is never included in any response body    |
| S-03 | Full user message content is never written to logs                       |
