# AGENTS

Generic rules for Go AWS Lambda services. Project-specific context lives in `spec/`.

---

## Code Structure

```
cmd/
  main.go        # Lambda entrypoint — composition root only; wires all deps
handler/
  handler.go     # Core request handling logic; receives all deps via constructor
internal/
  integrations/  # External integrations; each in its own package with client and tests
  repository/    # Database implementation and tests
```

> `cmd/main.go` is the **only** place where concrete implementations are created and wired together.
> No other file may construct concrete clients or read configuration.

---

## Interfaces

All external dependencies must be behind interfaces so they can be mocked in tests.
Interfaces are **owned by the consumer** — defined small, in the package that uses them.
All interface implementations must be injected via constructor.

---

## Coding Rules

These rules are **mandatory**. Code that violates them must be rewritten.

### Spec-First Requirement

- Before creating or changing any implementation, update the relevant docs under `spec/` to reflect the intended behavior, constraints, and acceptance criteria.
- Implementation work must not begin until the spec updates are completed and explicitly approved.

### Dependency Injection

- All external dependencies are injected via constructor — no global clients, no `init()`.
- No dependency may be created inside business logic or handler methods.
- `cmd/main.go` is the only place allowed to: read configuration, create concrete implementations, and wire dependencies together.

### Integrations (HTTP, DB, external APIs)

Each integration lives in its own package under `internal/` and must have:

| File                    | Purpose        |
|-------------------------|----------------|
| `<name>_client.go`      | Implementation |
| `<name>_client_test.go` | Tests          |

Rules for all integration clients:

- Every method takes `context.Context` as the first argument — ignoring context is not allowed.
- `http.Client` must have an explicit `Timeout` — `http.DefaultClient` is **forbidden**.
- The HTTP client must be injected via constructor and accessed via a private `httpClient()` helper — never inline nil-checked or duplicated across methods.
- Request and response shapes must be explicit `struct` types with JSON tags — `map[string]interface{}` is forbidden.
- External models must never be used as domain models; conversion between external and internal shapes must be explicit.
- All errors must be wrapped with context: `fmt.Errorf("pkg: operation: %w", err)`.
- Credentials fetched from external stores (e.g. SSM/ParamStore) must **never** be passed as constructor arguments — constructors must not accept API keys, tokens, or secrets in any form.
- Credentials are fetched lazily on the first method call using `sync.Once`, cached for the lifetime of the process (one Lambda cold start = one SSM fetch), and accessed via a private `resolveXxx(ctx)` helper. This is the only permitted pattern.
- The constructor only validates that required dependencies (e.g. a `paramstore.Getter`) are non-nil — it does no I/O and makes no network calls.
- Logic shared across two or more methods (auth, HTTP dispatch, URL building) must be extracted into private helpers — copy-pasting between methods is forbidden.

Defensive error handling — integrations must handle:

- `4xx` / `5xx` / `429`
- Timeout and network errors
- Empty body, malformed JSON, unexpected structure
- A `200` response is **not** a guarantee of a correct payload.

### Layer Rules

```
internal/
  domain/        # No IO. No knowledge of HTTP/DB. Must not import infrastructure.
  usecase/       # Orchestrates domain. Depends on interfaces only. No direct IO.
  integrations/  # Implements interfaces. Does IO. Contains no business rules.
```

Dependency direction: `usecase` → `domain`; `integrations` → interfaces defined by `usecase`/`domain`.
`domain` must never import `infrastructure`.

### Forbidden

- Global state or singleton clients
- `http.DefaultClient`
- Creating clients inside methods
- Reading environment variables in the domain layer
- Untested integrations
- Dependency from `domain` → `infrastructure`

---

## Testing

- **Unit tests:** mock all interfaces; test handler logic in isolation.
- **Integration tests:** exercise the handler end-to-end against local stubs or test doubles.
- **Acceptance tests:** Gherkin/Godog recommended; place feature files under `features/` and step definitions under `features/steps/`.

Each integration **must** test:

- Happy path
- Timeout
- `4xx` / `5xx` / `429` — **every public method must have its own 429 and 5xx test**, not just one method in the client
- Malformed JSON
- Network error

Testing only the happy path is not allowed. Use `httptest.Server` for HTTP-based integrations.
Test coverage must be **symmetric** — if a test case exists for one method, the equivalent case must exist for all methods that share the same failure surface.

Run tests:
```bash
go test ./...
```

---

## Error Handling

- Validate event format early — unsupported format → `400`.
- Missing/invalid fields → `400` with a descriptive error code.
- Upstream failures → `502` or `500` depending on cause; include a correlation ID in logs and responses.
- Do not log secrets or full user messages.

---

## Build & Run

The `Makefile` provides the main workflow:

| Target       | Description                                                   |
|--------------|---------------------------------------------------------------|
| `make`       | Clean, build Linux/arm64 binary, run tests                    |
| `make build` | Compile static Linux/arm64 binary to `./cmd/build/bootstrap`  |
| `make test`  | Run `go test ./...`                                           |
| `make zip`   | Create `./cmd/build/function.zip` (run after `make build`)    |
| `make clean` | Remove `./cmd/build/`                                         |

To build for your host:
```bash
GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) make build
```
