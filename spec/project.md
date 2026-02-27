# spec: project
```
service: personal-ai-agent
version: 1.0
file:    project
```
Accepts a single user question and returns an AI-generated answer based on the portfolio owner's resume, interests, and prior conversation history. The HTTP handler is a thin transport adapter that delegates request orchestration to an application use case. The question is validated, persisted in DynamoDB together with conversation metadata in one atomic state write, and the answer is returned in a structured format.
---
## System Structure
| Component      | Responsibility                                                                              |
|----------------|---------------------------------------------------------------------------------------------|
| `handler`      | HTTP/Lambda transport only; parses requests, maps use case errors, sets headers             |
| `usecase`      | Main ask workflow; validates input, loads config, moderates, builds prompts, persists state |
| `domain`       | Shared provider-agnostic models                                                             |
| `repository`   | Conversation persistence; owns DynamoDB record and key construction                         |
| `integrations` | External calls to SSM and OpenAI                                                            |

---
## Runtime Model
| Area        | Description                                                                                                                                                                                            |
|-------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| State       | Conversation history is stored as completed user-turn records with user `text` and assistant `answer` together                                                                                         |
| Consistency | Conversation turn metadata is written atomically with each successful turn                                                                                                                             |
| Prompt      | The request uses one policy system message, one profile-context system message, completed history replayed as user/assistant pairs, and a structured JSON output contract with `in_scope` and `answer` |
---
## Spec Index
| File                          | Purpose                                              |
|-------------------------------|------------------------------------------------------|
| `spec/project.md`             | Description, dependencies and spec index (this file) |
| `spec/interfaces/post-ask.md` | POST /ask — contract, validation, examples           |
| `spec/acceptance-criteria.md` | Testable criteria grouped by concern                 |
| `spec/infrastructure.md`      | Compute, storage, networking, IAM, env vars          |
| `spec/observability.md`       | Logs and metrics                                     |
