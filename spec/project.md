# spec: project
```
service: personal-ai-agent
version: 1.0
file:    project
```
Accepts a single user question and returns an AI-generated answer based on the portfolio owner's resume, interests, and prior conversation history. The HTTP handler is a thin transport adapter that delegates request orchestration to an application use case. The question is validated, persisted in DynamoDB together with conversation metadata in one atomic state write, and the answer is returned in a structured format.
---
## Dependencies
| Dependency | Purpose                                                                                               |
|------------|-------------------------------------------------------------------------------------------------------|
| handler    | HTTP/Lambda transport only; request/response mapping                                                  |
| usecase    | Application orchestration for the ask workflow, with prompt helpers split from the main workflow file |
| paramstore | Load resume, interests, pinned prompt                                                                 |
| openai     | Safety moderation and combined recruiting-relevance+answer generation                                 |
| statestore | Read and write completed user-turn conversation history; owns storage key construction                |
---
## Answer Constraints
| Constraint  | Value                                            |
|-------------|--------------------------------------------------|
| Source      | Only resume, interests, and conversation history |
| Fabrication | Not allowed                                      |
| Unknown     | Respond: *"I don't have that information."*      |
| Tone        | professional                                     |
| Perspective | first person                                     |
| Verbosity   | concise                                          |
---
## Spec Index
| File                          | Purpose                                              |
|-------------------------------|------------------------------------------------------|
| `spec/project.md`             | Description, dependencies and spec index (this file) |
| `spec/interfaces/post-ask.md` | POST /ask — contract, validation, examples           |
| `spec/acceptance-criteria.md` | Testable criteria grouped by concern                 |
| `spec/infrastructure.md`      | Compute, storage, networking, IAM, env vars          |
| `spec/observability.md`       | Logs and metrics                                     |
