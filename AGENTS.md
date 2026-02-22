# AGENTS

## Purpose
This service is an AWS Lambda-backed API that answers questions about the portfolio owner using an LLM. API Gateway receives HTTP requests and invokes a Go Lambda which:
- normalizes API Gateway v1/v2 events into a canonical request shape
- loads contextual data (resume, interests, pinned prompts) from SSM Parameter Store
- reads/writes conversation/agent state in DynamoDB
- builds a prompt envelope and calls an OpenAI-compatible client
- persists conversational state and returns a structured JSON response

Entrypoint: `cmd/lambda/main.go` (handler implemented in `handler/handler.go`).

## Quick architecture (text)
API Gateway (REST API v1) -> Lambda (Go) -> SSM Parameter Store -> DynamoDB -> OpenAI API -> Lambda -> API Gateway response

## Components & responsibilities
- API Gateway (v1 REST)
  - Exposes POST /ask (or similar) and forwards the event to Lambda.
  - Accepts client HTTP requests and provides a stable HTTP-facing API.

- Lambda (Go)
  - Normalize API Gateway v1/v2 events into a canonical internal request shape (method, path, headers, query, body, isBase64Encoded).
  - Validate inputs (question required, max length limits, etc.).
  - Load contextual parameters from SSM (resume, interests, pinned prompt templates).
  - Load and update agent/conversation state in DynamoDB (conversation history, last activity, pointers for idempotency).
  - Build a prompt envelope (system + context + history + user question) and call the OpenAI client through an interface so it can be mocked.
  - Persist derived metadata (summaries, tokens, last response) and return a structured JSON response.

- DynamoDB
  - Stores agent state and lightweight conversation transcripts.
  - Expect a single-table layout with partition key + sort key (e.g. PK: `CONV#<conversation_id>`, SK: `MSG#<rfc3339>` or `META#<property>`).
  - Use TTL for automatic cleanup and conditional writes for concurrency control.

- SSM Parameter Store
  - Stores larger context artifacts (resume, interests, pinned prompts) under a configurable prefix (env `PARAM_PREFIX`).
  - Lambda reads these at runtime (or caches when appropriate).

- OpenAI client
  - An implementation behind an interface. Keep the API key out of code (inject via environment variable).

## Data shapes
- Normalized request (internal)
  {
    "method": "POST",
    "path": "/ask",
    "headers": { "Content-Type": "application/json" },
    "query": { "lang": "en" },
    "body": { "question": "Who are you?" },
    "isBase64Encoded": false
  }

- DynamoDB item examples
  Conversation metadata:
  {
    "PK": "CONV#12345",
    "SK": "META#",
    "conversation_id": "12345",
    "last_activity": 1670000000,
    "turns": 12,
    "last_response_summary": "Answered about experience in Go",
    "ttl": 1730000000
  }

  Message record:
  {
    "PK": "CONV#12345",
    "SK": "MSG#2026-02-22T12:34:56Z",
    "role": "assistant",
    "text": "I am a software engineer...",
    "tokens": 123
  }

- Parameter Store keys (suggested)
  Prefix (configurable): `/portfolio-agent/` (env `PARAM_PREFIX`)
  - `/portfolio-agent/resume` — full text or JSON
  - `/portfolio-agent/interests` — list or CSV
  - `/portfolio-agent/pinned_prompt` — prompt template
  - `/portfolio-agent/config/openai_model` — default model name

- OpenAI prompt envelope (concept)
  {
    "system": "You are an assistant answering questions about a portfolio owner. Use the resume and interests as context.",
    "context": { "resume": "...", "interests": "..." },
    "history": [ { "role": "user", "content": "..." }, { "role": "assistant", "content": "..." } ],
    "user_question": "..."
  }

## Error handling
- Validate event format early; unsupported -> 400 with JSON { "ok": false, "error": "unsupported event format" }.
- Missing/invalid fields (e.g. question) -> 400 with a helpful message.
- Upstream failures (OpenAI, SSM, DynamoDB) -> 502 or 500 depending on cause; include a correlation id in logs and responses where appropriate.
- Avoid logging secrets or full user messages.

## Security and IAM
- Principle of least privilege: grant Lambda only the permissions it needs.
- Example env vars used by the runtime: `STATE_TABLE`, `PARAM_PREFIX`, `AWS_REGION`, `OPENAI_SECRET_ARN` (or `OPENAI_API_KEY`).
- Prefer Secrets Manager for OpenAI keys; SSM is for contextual artifacts (not secrets).

## Operational concerns
- Retries: use bounded exponential backoff for transient errors (SSM/Dynamo/OpenAI).
- Idempotency: accept a conversation_id or client message_id to dedupe; store message -> response mappings in DynamoDB.
- Concurrency: use conditional writes (UpdateItem with condition expressions) to avoid overwrite races.
- TTL: set DynamoDB TTL for stale conversations.
- Cost: avoid sending full large documents repeatedly; consider summaries, embeddings, or external indexing.
- Observability: structured logs (correlation_id/request_id), and metrics for latency and upstream errors.

## Testing
- Unit tests: mock SSM/Dynamo/OpenAI interfaces.
- Integration tests: exercise handler end-to-end against a local stub or test doubles.
- Acceptance tests: Gherkin/Godog is recommended; place features under `features/` and step defs under `features/steps/`.

## Local run
- Run unit tests: `go test ./...`
- Run the lambda locally (example): `go run ./cmd/lambda` or use a small local runner that creates a sample normalized event.

## Makefile / Build & Test
This repository includes a top-level `Makefile` that provides a small build/test workflow optimized for producing a Linux-compatible Lambda binary and running the project's tests. The important points:

- Default behavior: running `make` (or `make all`) will run the `build` target followed by the `test` target. In short: `make` => build + test.
- Targets you can use:
  - `make` or `make all` — clean, build the Linux/amd64 binary and run the test suite.
  - `make build` — compile a static Linux/amd64 binary to `./cmd/build/bootstrap` (this target depends on `clean`).
  - `make test` — run `go test ./...`.
  - `make zip` — create `./cmd/build/function.zip` containing the built `bootstrap` binary (use after `make build`).
  - `make clean` — remove the `./cmd/build` output directory.

- Environment / cross-build notes:
  - The `Makefile` sets `GOOS=linux` and `GOARCH=amd64` by default so the produced binary is suitable for AWS Lambda. If you want to build for your host or a different platform, override the variables on the command line, for example:

    `GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH) make build`

  or explicitly:

    `GOOS=linux GOARCH=amd64 make build`

- Recommended workflow:
  1. Run `make` to ensure the project builds and all tests pass before creating a zip artifact. This enforces the build+test step every time you run the top-level build command.
  2. Optionally run `make zip` to create `./cmd/build/function.zip` for deployment.

- Example quick commands:

  - Build and test (recommended):

    `make`

  - Only run tests:

    `make test`

  - Build and create ZIP artifact:

    `make build && make zip`

## Deployment notes
- Table name, parameter prefix, and secret ARNs must be configured via environment variables at deploy time.
- Follow least-privilege IAM policies for SSM, DynamoDB and Secrets Manager.

## Architecture documentation
- See `ARCH.md` for a detailed architecture view.
