# spec: infrastructure
```
service: personal-ai-agent
version: 1.0
file:    infrastructure
```
---
## Compute — Lambda
| Property     | Value          |
|--------------|----------------|
| Runtime      | `provided.al2` |
| Language     | Go             |
| Memory       | 1024 MB        |
| Timeout      | 29 seconds     |
| Architecture | arm64          |
### Environment Variables
| Variable                 | Source             | Value / Description                                              |
|--------------------------|--------------------|------------------------------------------------------------------|
| `STATE_TABLE`            | Terraform output   | DynamoDB table name                                              |
| `PARAM_PREFIX`           | Terraform variable | SSM prefix (e.g. `/portfolio-agent`)                             |
| `MAX_QUESTION_LENGTH`    | hardcoded          | `300`                                                            |
| `MAX_CONTEXT_ITEMS`      | hardcoded          | `20`                                                             |
| `MAX_CONVERSATION_TURNS` | hardcoded          | `10`                                                             |
---
## Network — API Gateway
| Property | Value                                            |
|----------|--------------------------------------------------|
| Type     | REST API (v1)                                    |
| Proxy    | Lambda                                           |
| Logging  | execution metrics enabled; body tracing disabled |
> Route, method, auth, and CORS are defined in `spec/interfaces/post-ask.md`.
---
## Storage — DynamoDB
| Property      | Value             |
|---------------|-------------------|
| Table name    | `agent-questions` |
| Partition key | `PK` (string)     |
| Sort key      | `SK` (string)     |
| Billing       | PAY_PER_REQUEST   |
| TTL attribute | `ttl`             |
### Item: Conversation Metadata (`SK: META#`)
| Field          | Type   | Constraints                                                             |
|----------------|--------|-------------------------------------------------------------------------|
| `lastActivity` | string | RFC3339 timestamp                                                       |
| `turns`        | number | integer >= 0; total successful in-scope user turns for the conversation |
| `ttl`          | number | Unix epoch seconds                                                      |
### Item: Message Record (`SK: MSG#<rfc3339>`)
| Field    | Type   | Constraints                                                         |
|----------|--------|---------------------------------------------------------------------|
| `text`   | string | non-empty user question                                             |
| `answer` | string | populated in the same write as the final successful user message    |
| `status` | string | `"complete"` for persisted successful user messages (see W-01-W-03) |
| `ttl`    | number | Unix epoch seconds                                                  |
---
## Config Store — SSM Parameter Store
| Key                            | Type         | Description                |
|--------------------------------|--------------|----------------------------|
| `<prefix>/resume`              | String       | Full text or JSON          |
| `<prefix>/interests`           | String       | List or CSV                |
| `<prefix>/pinned_prompt`       | String       | System prompt template     |
| `<prefix>/config/openai_model` | String       | Model name (e.g. `gpt-4o`) |
| `<prefix>/open-ai-token`       | SecureString | OpenAI API key             |
> Prefix controlled by env var `PARAM_PREFIX` (e.g. `/portfolio-agent`).
> `resume`, `interests`, `pinned_prompt`, and `config/openai_model` are required runtime parameters; missing values are treated as internal errors.
---
## IAM Permissions
| Service       | Actions                        |
|---------------|--------------------------------|
| DynamoDB      | `GetItem`, `PutItem`, `Query`, `TransactWriteItems` |
| SSM           | `GetParameter`                 |
| OpenAI (HTTP) | outbound HTTPS (no IAM action) |
