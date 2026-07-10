# Go Code Guidelines: Unified Summary

Synthesized from Ben Johnson, Gary Bernhardt, Mat Ryer, Michael Feathers, Mitchell Hashimoto,
Martin Kleppmann, John Ousterhout, Gregor Hohpe, Bobby Woolf, the SRE Book authors
(Betsy Beyer, Chris Jones, Jennifer Petoff, Niall Richard Murphy), and Go CLI command patterns
(stdlib, ff/v4, Cobra).

**Eight organizing principles:**

1. Complexity is the enemy — reduce it, hide it, push it down.
2. Separate computation from mutation. Pure core; thin imperative shell.
3. Dependencies are explicit. No globals; no hidden state.
4. Data systems fail in predictable ways — design for the failure modes, not the happy path.
5. Tests are a design tool, not a coverage metric.
6. Integration coupling is multi-dimensional — couple by accident, decouple with intent, and name which dimensions changed.
7. Reliability is a feature — measure it, budget for it, and protect the engineering capacity to improve it.
8. A CLI is a composition root — `main` wires dependencies; commands dispatch work; neither owns business logic.

______________________________________________________________________

## 1. Project Layout

**Do not:**

- Group packages by role (`models/`, `controllers/`, `handlers/`) — causes circular dependency problems.
- Put domain types in subpackages — until a domain grows large enough that a single file becomes unwieldy, at which point reorganizing that domain into its own named subpackage is correct. The flat root-package layout is the right starting point; split into named domain subpackages only when size demands it.
- Allow the root package to import any other application package.
- Put business logic in `main`.
- Put the `main` package in the project root — **when the project root is a domain package** (e.g. `package myapp` with types, interfaces, and an `Error` type). If the project is a pure CLI tool with no domain package, `main.go` belongs at the root and `cmd/` holds command sub-packages rather than binary entry points. Signal: if `go doc .` would show domain types and service interfaces, use `cmd/<name>/main.go`; if it would show command infrastructure, keep `main.go` at root.
- Use global variables for application state (DB connections, config, etc.).
- Let subpackages import each other — shared logic goes in the root package.

**Layout:**

```text
myapp/                    — root package: domain types, interfaces, Error type
    user.go               — User struct, UserService interface, UserFilter, UserUpdate
    error.go              — Error type, error codes, ErrorCode(), ErrorMessage()
    context.go            — NewContextWithUser(), UserIDFromContext()
    user_cache.go         — UserCache: caching wrapper implementing UserService
    cmd/
        myapp/main.go     — wires dependencies; zero business logic
        myappctl/main.go  — optional CLI binary
    sqlite/               — SQLite implementation of domain interfaces
        sqlite.go         — DB and Tx types, Open(), Close()
        user.go           — sqlite.UserService
    http/                 — HTTP adapter
        server.go         — Server struct, NewServer(), route registration
        user.go           — HTTP handlers for users
    mock/                 — hand-written mocks for testing
        user_service.go   — mock.UserService
```

**Rules:**

- Root package name matches the application name (`package myapp`).
- Each subpackage is named after the dependency it wraps: `sqlite`, `http`, `postgres`, `mock`.
- Shadowing a stdlib package name (`http`) is acceptable — the two never appear in the same file.
- One major concept per file. Target 200–500 SLOC; 1000 SLOC is the hard limit.
- `main` is the only place where concrete implementation packages are imported together. Dependency injection is manual — no framework.
- Each subpackage struct holds only the dependencies it needs.

______________________________________________________________________

## 2. Domain Types, Interfaces, and CRUD Conventions

The root package is the application's shared language. It contains only plain structs, service
interfaces, the `Error` type, and context helpers. No `database/sql`, `net/http`, or any
third-party import.

```go
type User struct {
	ID        int
	Name      string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserService interface {
	// FindUserByID retrieves a user by ID.
	// Returns ENOTFOUND if the user does not exist.
	FindUserByID(ctx context.Context, id int) (*User, error)

	// FindUsers retrieves users matching filter.
	// Returns the total matching count regardless of Limit/Offset.
	FindUsers(ctx context.Context, filter UserFilter) ([]*User, int, error)

	// CreateUser creates a new user.
	// On success, user.ID and timestamps are populated on the input struct.
	CreateUser(ctx context.Context, user *User) error

	// UpdateUser updates a user by ID.
	// Returns the updated user even on error (for form replay).
	// Returns ENOTFOUND if the user does not exist.
	// Returns EUNAUTHORIZED if the caller does not own the user.
	UpdateUser(ctx context.Context, id int, upd UserUpdate) (*User, error)

	// DeleteUser permanently removes a user by ID.
	// Returns ENOTFOUND if the user does not exist.
	// Returns EUNAUTHORIZED if the caller does not own the user.
	DeleteUser(ctx context.Context, id int) error
}

// Filter structs use pointer fields — nil means not set.
type UserFilter struct {
	ID     *int
	Email  *string
	Offset int
	Limit  int
}

// Update structs use pointer fields — nil means do not change.
type UserUpdate struct {
	Name  *string
	Email *string
}
```

**CRUD conventions:**

- `FindByID` never returns `(nil, nil)`. Missing entity returns `ENOTFOUND`.
- `FindMany` returns `([]*T, int, error)`. The `int` is total count for pagination. An empty result is not an error.
- `Create` accepts a pointer and mutates it in place (sets ID, CreatedAt, UpdatedAt).
- `Update` accepts a `*Update` struct with pointer fields so partial updates need no separate endpoint. Returns the updated object even on error — UIs need it to replay the form.
- `Delete` accepts only the primary key. Enforces authorization inside the implementation.
- Service interfaces live in the same file as the type they operate on.
- Every interface method has a godoc comment naming which error codes it can return.

### Layering via Wrapper Types

All implementations satisfy the same root-package interface, so they can be stacked. A caching
layer wraps any `UserService` without the callers knowing:

```go
// myapp/user_cache.go
type UserCache struct {
	cache   map[int]*User
	service UserService
}

func NewUserCache(service UserService) *UserCache {
	return &UserCache{cache: make(map[int]*User), service: service}
}

func (c *UserCache) FindUserByID(ctx context.Context, id int) (*User, error) {
	if u := c.cache[id]; u != nil {
		return u, nil
	}
	u, err := c.service.FindUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.cache[id] = u
	return u, nil
}

// Remaining methods delegate to c.service
```

`main` composes layers: `userService := myapp.NewUserCache(&sqlite.UserService{DB: db})`

______________________________________________________________________

## 3. Error Handling

**Do not:**

- Return `(nil, nil)` from any function that looks up a single entity by ID.
- Type-assert `*Error` directly in calling code — use `ErrorCode()` and `ErrorMessage()`.
- Let external errors (`sql.ErrNoRows`, `os.ErrNotExist`) escape the implementation boundary.
- Add catch-all error handlers at internal boundaries — hidden failures are hidden complexity.
- Hold transactions open across user interaction.
- Swallow errors silently. Return errors explicitly; let them propagate to where they can be meaningfully handled.

Define one `Error` type in the root package:

```go
const (
	ECONFLICT     = "conflict"     // action cannot be performed
	EINTERNAL     = "internal"     // internal error
	EINVALID      = "invalid"      // validation failed
	ENOTFOUND     = "not_found"    // entity does not exist
	EUNAUTHORIZED = "unauthorized" // caller lacks permission
)

type Error struct {
	Code    string // machine-readable; set only on leaf errors
	Message string // human-readable; set only on leaf errors
	Op      string // "package.Type.Method"; set only on wrapping errors
	Err     error  // nested cause; set only on wrapping errors
}
```

- A **leaf error** carries `Code` + `Message`. A **wrapping error** carries `Op` + `Err`. Never set both `Code` and `Err` on the same `Error`.
- Every significant function wraps errors with `Op` in the format `"package.Type.Method"`, producing a single-line logical stack trace:
  `sqlite.UserService.CreateUser: sqlite.insertUser: near "INSERT": syntax error`
- Translate all external errors to domain error codes at the implementation boundary — before they escape to callers.
- Use `ErrorCode(err)` and `ErrorMessage(err)` at call sites. Start with five codes; add more only as needed.

```go
func (s *UserService) CreateUser(ctx context.Context, user *myapp.User) error {
	const op = "sqlite.UserService.CreateUser"
	if err := s.insertUser(ctx, user); err != nil {
		return &myapp.Error{Op: op, Err: err}
	}
	return nil
}
```

**Unrecoverable errors:** crash rather than littering the code with checks that cannot improve
the outcome. Out-of-memory, internal inconsistency, and invariant violations are in this
category. Where possible, **define errors out of existence** — redesign semantics so the error
cannot occur ("ensure X does not exist" always succeeds; "delete X, error if absent" introduces
a failure mode).

**Aggregating exceptions:** let errors propagate to a single high-level handler rather than
writing distinct handlers at every call site. Only handle an error at the level where you can
do something meaningful with it.

______________________________________________________________________

## 4. Interface Design

- Source: *(Ousterhout)* A module's interface should be dramatically simpler than its implementation.

**Do:**

- Implement complexity (buffering, encoding, retries, locking) inside the module; expose none of it to callers. The best modules hide enormous complexity behind a small, clean surface.
- Make the common case require the fewest possible calls and the least prior knowledge.
- Provide defaults automatically — do the right thing without being asked. Callers should be unaware of options they do not need.
- Merge two modules when they share knowledge of the same design decision.
- Pull complexity downward: push edge cases, defaults, and normalization into the module. Before exporting a configuration parameter, ask: "Will callers actually know the right value better than I do here?"
- Split a method when a subtask is independently reusable and fully understandable without reading the parent. Join two shallow methods when doing so simplifies the interface or eliminates an intermediate data structure.
- Write interface comments **before** writing the body. If the comment is long and tangled, the interface is wrong. The act of writing the comment is a design check.
- Model domain constraints in types rather than in validation logic. A type with a validated constructor that can only hold values in `[1, 12]` eliminates the need to test that an integer is in range wherever it is used. The compiler enforces what it can; write tests only for what it cannot.

**Do not:**

- Write pass-through methods — methods whose entire body is a single forwarding call with the same signature.
- Split a method just to enforce an arbitrary line count. A long method with a simple interface is fine — it is deep.
- Create a class or function whose interface is nearly as complex as its body.
- Expose internal representation through getters/setters that return the actual internal structure.
- Use the Decorator pattern unless the decorator adds substantial functionality.
- Structure code around the time-order of operations when information hiding argues for different decomposition. Choose decomposition by knowledge, not by time.
- Put special-purpose code inside a general-purpose mechanism.
- Pass a variable through a long chain of methods that do not use it — store shared context in instance variables instead.
- Write tests that merely verify the compiler's type system is working. A test asserting that a function returns a string when a string is expected adds no value in a statically typed language.

**Red flags:**

| Red Flag                | Symptom                                                     |
| ----------------------- | ----------------------------------------------------------- |
| Shallow Module          | Interface not much simpler than implementation              |
| Information Leakage     | Same design decision reflected in multiple modules          |
| Temporal Decomposition  | Code mirrors execution order rather than knowledge cohesion |
| Pass-Through Method     | Method only forwards to another with same signature         |
| Overexposure            | Common API forces learning rarely needed features           |
| Special-General Mixture | General mechanism contains special-purpose code             |
| Conjoined Methods       | Cannot understand one without reading the other             |
| Repetition              | Same nontrivial code appears multiple places                |
| Vague Name              | Name too broad to convey meaning                            |
| Hard to Pick Name       | Difficulty naming reveals unclear purpose                   |
| Comment Repeats Code    | Comment says only what the code already shows               |
| Nonobvious Code         | Behavior cannot be understood quickly                       |

______________________________________________________________________

## 5. Functional Core / Imperative Shell

- Source: *(Bernhardt, Feathers)* The amount of code that mixes computation with state change determines
how hard a system is to test and reason about. Push computation into a pure core; confine all
mutation and side effects to a thin outer shell.

**Pure core — Do:**

- Write domain functions as transformations: accept values, return new values.
  `func (t Timeline) WithNewTweets(tweets []Tweet) Timeline` — not a mutation.
- Accept dependencies as parameters rather than fetching them internally.
  A function that accepts `[]Rating` is testable; one that calls `db.Query(...)` inside is not.
- Use local variable rebinding (`result = compute(result)`) freely inside a function body — it is invisible from outside.

**Pure core — Do not:**

- Add methods to domain types that mutate any field on the receiver.
- Mutate a slice or map passed in as a parameter — return a new one.
- Put a database call, network request, or `time.Now()` inside a function that also contains business logic. Move the I/O to the caller and pass the result in.
- Design a domain function so that testing it requires a database fixture or network stub. If it does, the function is not pure — split it.

**Imperative shell — Do:**

- Write shell code as a flat sequence of assignments and I/O calls with minimal branching.
- Keep the shell as the sole point of contact with each external resource — serializes access by structure, not by locking.
- Designate a single goroutine as the sole owner of each external resource:

```go
// Background goroutine produces an immutable value and sends it
go func() {
	tweets := fetchTweets() // produces immutable []Tweet
	results <- Timeline{Tweets: tweets}
}()

// Owner goroutine receives and assigns; it alone does the writing
newTimeline := <-results
db.Save(newTimeline) // only the owner goroutine writes
cursor = newTimeline // only the owner goroutine mutates state
```

**Imperative shell — Do not:**

- Put computed logic (sorting, filtering, ranking) in the shell — pass values to a pure function and use the result.
- Update state from multiple places. One mutable reference, one assignment site.

**Testing the shell:**
The shell needs no tests when it is a flat sequence of assignments and I/O calls with few
conditionals. **This is a judgment calibrated to current complexity, not an absolute rule.**
Add shell tests when it develops branches, accumulates logic, or reaches a size where silent
misbehavior becomes plausible. The threshold is fear: test what you are afraid of. Do not let
the shell absorb logic on the grounds that it will not be tested — that logic is technical debt.

**Evolving the shell — two-commit sequence:**

1. Write rough, untested exploratory code in the shell when the design is genuinely unknown.
2. Once the design is clear, write the new core function via TDD with full test coverage — **before** wiring it in. **Commit 1: new function only — nothing else changes.**
3. **Commit 2: swap.** Wire the new function in and delete all the shell code it replaces. This commit should show a large deletion. If it does not, the extraction is incomplete.

The rhythm: bloat → extract via TDD → delete → shrink. The shell is not a permanent home for logic that has been understood.

**Incremental application:**

- Ask of every new function: can this be pure? If behavior depends only on parameters, make it accept those parameters explicitly and return a value.
- Treat test difficulty as a locating device: a hard-to-write test is pointing at a boundary that should move. Move the boundary; the test becomes easy.

______________________________________________________________________

## 6. Context and Authorization

```go
// myapp/context.go
type contextKey int

const userContextKey contextKey = iota

func NewContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey).(*User)
	return u
}

func UserIDFromContext(ctx context.Context) int {
	if u := UserFromContext(ctx); u != nil {
		return u.ID
	}
	return 0
}
```

- The transport layer sets the user on the context after authenticating.
- Service implementations extract the user from context and enforce authorization.
- Enforce authorization at the lowest level — embedded in SQL `WHERE` clauses so the database engine enforces it, not application-level filtering after the fact.
- Do not enforce authorization in HTTP handlers or middleware.

```go
// Authorization embedded in the query, not in application code after the fact
where = append(where, `id IN (SELECT dial_id FROM dial_memberships WHERE user_id = ?)`)
args = append(args, myapp.UserIDFromContext(ctx))
```

______________________________________________________________________

## 7. HTTP Services

**Do not:**

- Use global variables for dependencies.
- Use the global `flag` package — use `flag.NewFlagSet` inside `run`.
- Use `t.SetEnv` in tests — it disables `t.Parallel()`. Use the `getenv` parameter instead.
- Store durable state in handler closures — closures do not survive restarts and do not scale across instances. Use a database.
- Call `os.Exit` anywhere except `main`.
- Put fallible setup in `addRoutes` — resolve errors in `run` first.
- Use a named type alias for middleware — `func(http.Handler) http.Handler` is explicit enough.
- Call `json.NewEncoder` or `json.NewDecoder` inline in handlers — use central helpers.
- Repeat middleware dependency arguments on every `mux.Handle` call — use a constructor.

**File layout:**

```text
main.go       — main() only; calls run()
run.go        — run() function; wires dependencies, starts server
server.go     — NewServer() constructor
routes.go     — addRoutes(); every mux.Handle call lives here
middleware.go — middleware constructor functions
encode.go     — encode, decode, decodeValid generic helpers
```

### Entry Point

Two valid shapes exist. Choose based on whether `run()` owns the program lifecycle or delegates it to a dispatcher.

#### Shape a — `run()` as the Program (HTTP Services, Standalone Daemons)

`run()` accepts all OS primitives as parameters and returns `error`. It owns flag parsing, dependency wiring, and process lifecycle.
`signal.NotifyContext` goes inside `main`, not `run`, so `stop` is deferred correctly.

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM,
	)
	defer stop()
	if err := run(ctx, os.Args, os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		// After the first call, subsequent calls to a CancelFunc do nothing
		stop()
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	// parse flags from args[1:], build deps, call NewServer, start httpServer
}
```

**`run` parameter reference — what `main` passes vs. what tests pass:**

| Parameter | Type                  | `main` passes                                     | Test passes               |
| --------- | --------------------- | ------------------------------------------------- | ------------------------- |
| `ctx`     | `context.Context`     | `signal.NotifyContext(context.Background(), ...)` | `context.WithCancel(...)` |
| `args`    | `[]string`            | `os.Args`                                         | custom `[]string`         |
| `getenv`  | `func(string) string` | `os.Getenv`                                       | custom func               |
| `stdin`   | `io.Reader`           | `os.Stdin`                                        | `strings.NewReader(...)`  |
| `stdout`  | `io.Writer`           | `os.Stdout`                                       | `&bytes.Buffer{}`         |
| `stderr`  | `io.Writer`           | `os.Stderr`                                       | `io.Discard`              |

Testability: call `run(ctx, args, ...)` directly with injected I/O.

#### Shape B — `run()` as Exit-Code Translator (Dispatcher / Command Pattern, Ff/v4, Climax)

`run()` accepts `ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer`, and returns `int`.
It calls a command dispatcher that itself accepts I/O as parameters; `run()` converts the dispatcher's error into an exit code.
`signal.NotifyContext` belongs in `main()`, not `run()` — because `run()` returns an `int` and `main()` must call `stop()` before `os.Exit(code)`. If `run()` called `os.Exit` directly, `stop()` would never execute, leaving the signal-handler goroutine alive until process death.

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM,
	)
	code := run(ctx, os.Args, os.Stdin, os.Stdout, os.Stderr)
	stop() // release signal goroutine before Exit
	os.Exit(code)
}

// run is intentionally separated from main to improve testability. Please preserve this comment.
func run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	err := cmd.Run(ctx, args[1:], stdin, stdout, stderr)
	var exitErr root.ExitError
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		return exitSuccess
	case errors.As(err, &exitErr):
		return int(exitErr)
	default:
		_, _ = fmt.Fprintf(stderr, "error: %+v\n", err)
		return exitFail
	}
}
```

Testability: two entry points are available. Call `run(ctx, args, stdin, stdout, stderr)` with injected I/O to test exit-code translation end-to-end — pass the full args slice including `args[0]` (program name), as `run()` strips it before forwarding to `cmd.Run` (the same pattern as Shape A). Call `cmd.Run(ctx, args, stdin, stdout, stderr)` directly with the already-stripped args slice to test the dispatcher in isolation, bypassing exit-code translation.

### Server Constructor

```go
func NewServer(logger *Logger, store *Store) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, logger, store)
	var handler http.Handler = mux
	handler = loggingMiddleware(handler) // global middleware applied here
	handler = someMiddleware(handler)
	return handler
}
```

- Return type is `http.Handler`, not a named struct, unless genuinely required.
- Global middleware (CORS, auth, logging) applied in `NewServer`. Per-route middleware in `addRoutes`.
- Pass `nil` for dependencies a particular test does not exercise.

### Routes

```go
func addRoutes(mux *http.ServeMux, logger *Logger, store *Store) {
	mux.Handle("/api/v1/users", handleUsersGet(logger, store))
	mux.Handle("/admin", adminOnly(handleAdminIndex(logger)))
	mux.HandleFunc("/healthz", handleHealthz(logger))
	mux.Handle("/", http.NotFoundHandler())
}
```

- Always register an explicit `http.NotFoundHandler()` for `/`.
- Always include `/healthz` or `/readyz`. Per-route middleware is applied inline and visible at a glance.
- `addRoutes` never returns an error.

### Handlers (Maker Func Pattern)

```go
func handleSomething(logger *Logger, store *Store) http.Handler {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Greeting string `json:"greeting"`
	}
	thing := prepareThing() // one-time setup at registration, not per-request

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := decode[request](r)
		if err != nil {
			encode(w, r, http.StatusBadRequest, errorResponse(err))
			return
		}
		encode(w, r, http.StatusOK, response{Greeting: "Hello " + req.Name})
	})
}
```

- Return type is `http.Handler`, not `http.HandlerFunc`. Third-party libraries expect `http.Handler`.
- Declare request/response types inside the maker func when only that handler uses them.
- One-time setup goes in the outer function scope, not inside the closure.
- Use `sync.Once` for expensive deferred setup; check the error outside `init.Do` so it surfaces on every request, not just the first.
- Only read shared closure state from concurrent handlers. Protect writes with a mutex.

### Middleware with Dependencies

<!-- mdformat off -->

```go
// middleware.go
func newAuthMiddleware(logger Logger, db *DB) func(http.Handler) http.Handler {
    return func(h http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // use logger, db
            h.ServeHTTP(w, r)
        })
    }
}

// routes.go — constructor called once; deps not repeated per route
auth := newAuthMiddleware(logger, db)
mux.Handle("/route1", auth(handleSomething(deps)))
mux.Handle("/route2", auth(handleSomethingElse(deps)))
```

<!-- mdformat on -->

### Encoding

```go
func encode[T any](w http.ResponseWriter, r *http.Request, status int, v T) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func decode[T any](r *http.Request) (T, error) {
	var v T
	return v, json.NewDecoder(r.Body).Decode(&v)
}

type Validator interface {
	Valid(ctx context.Context) (problems map[string]string)
}

func decodeValid[T Validator](r *http.Request) (T, map[string]string, error) {
	v, err := decode[T](r)
	if err != nil {
		return v, nil, err
	}
	if problems := v.Valid(r.Context()); len(problems) > 0 {
		return v, problems, fmt.Errorf("invalid %T: %d problems", v, len(problems))
	}
	return v, nil, nil
}
```

- `Valid` returns `nil` (not an empty map) when there are no problems. `len(nil map) == 0` and does not panic.
- Keep `Valid` to field-level checks. Database-dependent checks belong outside.

### Error Translation

```go
func errorStatusCode(err error) int {
	switch myapp.ErrorCode(err) {
	case myapp.ENOTFOUND:
		return http.StatusNotFound
	case myapp.EINVALID:
		return http.StatusBadRequest
	case myapp.EUNAUTHORIZED:
		return http.StatusUnauthorized
	case myapp.ECONFLICT:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
```

### Graceful Shutdown

```go
go func() {
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "listen error: %s\n", err)
	}
}()
var wg sync.WaitGroup
wg.Add(1)
go func() {
	defer wg.Done()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "shutdown error: %s\n", err)
	}
}()
wg.Wait()
```

______________________________________________________________________

## 8. SQL and Data Access

> **pgx/sqlc note (this repo):** This section's examples use `database/sql` (SQLite, `*sql.Tx`,
> `ExecContext`). This repo uses `pgx/v5` + `sqlc` with the pgx driver. Where the examples show
> `database/sql` APIs, use the pgx equivalents: `pool.Begin(ctx)` instead of `db.BeginTx(ctx, nil)`;
> `defer tx.Rollback(ctx)` and `tx.Commit(ctx)` (both take a context); `tx.Exec(ctx, ...)` not
> `tx.ExecContext(ctx, ...)`; pass `pgx.Tx` or `districtsql.DBTX` to helpers, not `*sql.Tx`
> (`pgx.Tx` satisfies `districtsql.DBTX`; use `sqldb.DBTX` for service-level dependencies that
> also need to call `Begin`). See the `pgxpool` and `sqlc` skills for repo-specific patterns.

**Do not:**

- Use an ORM — use `database/sql` directly. For PostgreSQL specifically, `pgx` and `sqlc` are the preferred alternatives; do not use `pgx` with non-PostgreSQL databases.
- Expose transactions to callers of a service — they are an implementation detail; `*Tx` or `*sql.Tx` must not appear in service method signatures.
- Omit `defer tx.Rollback(ctx)` immediately after a successful `Begin`/`BeginTx` — without it a failed commit or a panic leaves the transaction open and holds database locks. (`pgx.Tx.Rollback` takes a context; `database/sql`'s does not.)
- Interpolate caller-supplied strings into SQL queries.
- Allow arbitrary sort columns from callers — map a fixed set of named values to SQL.
- Initialize result slices with `var` — nil slices encode as JSON `null`; use `make([]*T, 0)`.
- Issue one query per record to load related data (N+1) when using a remote database — batch the association load or use a JOIN; per-record loading is acceptable only for embedded databases where each call has negligible round-trip cost.
- Use a relational database as a message queue — polling causes table-scan load and lock contention.

### Transaction Boundary Pattern

Service methods own the transaction boundary; unexported package-level helper functions own
the SQL. Helpers accept `*Tx` so multiple service methods can compose them within one transaction.

```go
// Service method: thin — begin tx, call helpers, commit
func (s *DialService) CreateDial(ctx context.Context, dial *myapp.Dial) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := createDial(ctx, tx, dial); err != nil {
		return err
	}
	return tx.Commit()
}

// Helper: unexported, accepts *Tx, reusable across service methods
func createDial(ctx context.Context, tx *Tx, dial *myapp.Dial) error {
	const op = "sqlite.createDial"
	result, err := tx.ExecContext(ctx, `
        INSERT INTO dials (user_id, name, invite_code, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?)`,
		dial.UserID, dial.Name, dial.InviteCode, dial.CreatedAt, dial.UpdatedAt,
	)
	if err != nil {
		return &myapp.Error{Op: op, Err: err}
	}
	dial.ID, err = result.LastInsertId()
	return err
}
```

### Row Iteration

Three invariants: `defer rows.Close()` immediately after a successful query; initialize with
`make([]*T, 0)`; return `rows.Err()` after the loop.

```go
rows, err := tx.QueryContext(ctx, query, args...)
if err != nil {
	return nil, 0, err
}
defer rows.Close()

dials := make([]*myapp.Dial, 0)
var n int
for rows.Next() {
	var d myapp.Dial
	if err := rows.Scan(&d.ID, &d.Name, &n); err != nil {
		return nil, 0, err
	}
	dials = append(dials, &d)
}
return dials, n, rows.Err()
```

### Dynamic WHERE Clauses

```go
where := []string{"1 = 1"} // never empty; strings.Join always produces valid SQL
args := []interface{}{}

if v := filter.ID; v != nil {
	where = append(where, "id = ?")
	args = append(args, *v)
}
if v := filter.Email; v != nil {
	where = append(where, "email = ?")
	args = append(args, *v)
}

// COUNT(*) OVER() returns total count on every row without a second query
query := `SELECT id, name, email, COUNT(*) OVER() FROM users WHERE ` +
	strings.Join(where, " AND ") + ` ORDER BY ` + orderBy + ` LIMIT ?`
args = append(args, filter.Limit)
```

### Sort Order: Fixed Named Values Only

```go
var orderBy string
switch filter.SortBy {
case "name_asc":
	orderBy = "name ASC"
case "updated_at_desc":
	orderBy = "updated_at DESC"
default:
	orderBy = "id ASC"
}
```

Never interpolate caller-supplied strings into SQL. Map a closed set of named values to SQL
fragments. Always provide a safe default.

### Loading Associations

Use a dedicated `attachXAssociations` helper after the primary query — reusable across
`FindByID` and `FindMany`. Always return parent associations; include child collections only
when they are small and almost always needed. When using a remote database server, batch the
association queries rather than issuing one query per record; per-record is acceptable for
embedded databases (e.g., SQLite) where each query has negligible round-trip cost.

```go
func (s *DialService) FindDials(ctx context.Context, filter myapp.DialFilter) ([]*myapp.Dial, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	dials, n, err := findDials(ctx, tx, filter)
	if err != nil {
		return dials, n, err
	}

	for _, dial := range dials {
		if err := attachDialAssociations(ctx, tx, dial); err != nil {
			return dials, n, err
		}
	}
	return dials, n, nil
}

func attachDialAssociations(ctx context.Context, tx *Tx, dial *myapp.Dial) error {
	var err error
	if dial.User, err = findUserByID(ctx, tx, dial.UserID); err != nil {
		return fmt.Errorf("attach dial user: %w", err)
	}
	return nil
}
```

### Pagination

Use cursor-based pagination — consistent under concurrent writes:

```sql
SELECT id, name FROM dials WHERE user_id = ? AND id > ? ORDER BY id ASC LIMIT ?
```

Offset-based (`LIMIT n OFFSET k`) skips rows inserted between fetches and returns
inconsistent results when rows are deleted — avoid it.

### Transaction Isolation

Default database isolation ("read committed" in most databases) does **not** prevent write
skew. Write skew: two concurrent transactions each read a value, make a decision based on it,
and write back — but together their writes violate a constraint that each read independently.
Examples: double-booking a seat, overdrafting an account, two users each grabbing the last
item.

- Use **serializable isolation** (PostgreSQL `SERIALIZABLE`, SSI in CockroachDB) for any
  operation that follows a "check then act" pattern across multiple records. Snapshot isolation
  (`REPEATABLE READ`) prevents non-repeatable reads but does **not** prevent write skew on
  multi-row checks.
- Prefer **Serializable Snapshot Isolation (SSI)** over two-phase locking (2PL). SSI detects
  conflicts at commit time with a small performance overhead; 2PL blocks readers under
  contention.
- Use **atomic operations** (`UPDATE counter = counter + 1`) instead of read-modify-write
  cycles in application code. ORMs make it easy to accidentally produce unsafe
  read-modify-write cycles.
- Keep transactions **short**. Long-running read-write transactions under SSI accumulate a
  large conflict footprint and have much higher abort rates. Perform expensive computation
  outside the transaction; use the result as input to a short transactional step.
- **Retry** transactions that fail with a serialization or deadlock error using exponential
  backoff. Distinguish transient failures (retry) from permanent errors (constraint violation
  — do not retry).
- For operations that clients may retry (e.g., after a network timeout where the commit
  succeeded but the response was lost), persist a unique **idempotency key** in the same
  transaction. On retry, look up the key first and return the cached result instead of
  re-executing.

| Hazard                                | Prevented by                               |
| ------------------------------------- | ------------------------------------------ |
| Dirty reads / writes                  | Read committed (default in most DBs)       |
| Non-repeatable reads                  | Snapshot isolation (Repeatable Read)       |
| Lost updates (concurrent increment)   | Atomic ops or explicit `SELECT FOR UPDATE` |
| Write skew (multi-row check then act) | Serializable only                          |
| Phantom reads                         | Serializable only                          |

______________________________________________________________________

## 9. Testing Philosophy

- Source: *(Feathers)* Before writing a test, ask: what are you actually trying to accomplish?
Testing serves three distinct goals:
- **Quality** — forcing deliberate thought before writing code (TDD, design-by-contract). The mechanism is the concentrated thinking, not the test catching a bug. Any practice that forces you to specify intent before writing code tends to produce similar quality results.
- **Maintenance** — establishing behavioral invariants so code can be changed safely (regression suites, characterization tests).
- **Validation** — confirming the software is acceptable to users. For tended systems with low domain risk, progressive production rollout is a legitimate validation strategy — **with the accountability condition**: the developer who ships the code must be the person who handles the consequences. That closed feedback loop changes behavior in ways that a QA gate does not. Do not apply this strategy to untended systems, regulated domains, or cases where failures cannot be quickly reversed.

**Do not** write tests to satisfy a coverage metric. Do not equate high test coverage with good
design. A codebase can have 90% coverage and still be deeply coupled, hard to change, and
poorly specified. Coverage measures what was exercised; it does not measure whether the
design is any good.

### Specify Intent Before Writing Code

Before implementing any non-trivial function, write a contract comment:

<!-- mdformat off -->

```go
// Requires: ratings is non-empty; each rating is in [1, 5]
// Ensures:  returns the top-N restaurants sorted descending by average rating;
//           len(result) <= min(n, len(ratings))
func TopRated(ratings []Rating, n int) []Restaurant { ... }
```

<!-- mdformat on -->

If `Requires` needs more than one sentence, the function has too many entry conditions — split
it. If `Ensures` requires enumerating many cases, the function does too much — decompose it.
Complex specifications mean complex code; simplify the design until the specification shrinks.

### Tests as Design Pressure

Tests that are painful to write are reporting a design problem. The correct response is to fix
the design, not to work around the test difficulty.

- Use mocks at the interaction boundary — the point where one object hands off to another —
  to force explicit specification of that interface **before implementing it**. The mock is a
  design tool; it documents the contract the collaborator must satisfy. When mock-intensive
  isolation testing is done with this intent, it achieves the same quality mechanism as TDD:
  thinking carefully about every interaction before implementing it.
- Do not work around testability problems by adding test-only seams, making fields
  package-visible, or restructuring production code solely to accommodate the test framework.
  Fix the design instead.
- Do not use mocks to achieve coverage of code that is already written. Mocking as coverage
  scaffolding is a design-smell response, not a design-pressure response.
- Use property-based tests to apply stronger design pressure than example-based tests. If the
  properties of a function are hard to articulate or require many exceptions, the function is
  too complex. Decompose it until each piece has clean, independently verifiable properties.

**Testing red flags:**

| Red Flag                       | What it means                                                                                                                                       |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hard-to-test code              | Writing a test requires mocking five collaborators or accessing private state — module boundaries are wrong                                         |
| Unmockable design              | Function cannot be called in a test without triggering DB connections, network, or file I/O — function does too much                                |
| Test fighting back             | Every attempt to write the test reveals a new dependency to stub — stop and redesign                                                                |
| Mocking implementation details | Mock must know the internal call sequence inside the function, not just the interface it calls through — abstraction boundary is in the wrong place |
| Tests that get turned off      | A suite teams routinely skip or disable has already been rejected as a feedback mechanism — fix the tests or the system, not the discipline         |

### Calibrate Investment by System Type

**The primary decision: is the system tended or untended?** Tended — someone can roll back,
patch, or intervene. Untended — cannot be changed after deployment (firmware, regulated systems).

| System   | Lifetime | Risk                  | Suggested posture                                                 |
| -------- | -------- | --------------------- | ----------------------------------------------------------------- |
| Untended | Long     | High (health/finance) | Full verification before any release                              |
| Untended | Long     | Low                   | Strong test coverage; no prod shortcuts                           |
| Untended | Short    | Any                   | Write intended functions first; get it right; no regression suite |
| Tended   | Long     | High                  | Strong coverage + production monitoring                           |
| Tended   | Long     | Low                   | TDD for design; regression for changes                            |
| Tended   | Short    | High                  | Strong coverage on core paths; skip UI                            |
| Tended   | Short    | Low                   | Deliberate design; minimal formal tests                           |

Do not apply a uniform testing standard regardless of lifetime, criticality, or recoverability.
Do not maintain a test suite for retired code — delete it with the feature.
If a test suite is slow enough that teams turn it off, it has already been rejected as a
feedback mechanism. Speed it up, cut it, or replace it. Do not enforce discipline to run it.

______________________________________________________________________

## 10. Test Structure and Mechanics

- Source: *(Hashimoto, Ben Johnson, Ryer)*

**Do not:**

- Use third-party testing frameworks — use the stdlib `testing` package only.
- Return errors from test helpers — call `t.Fatalf` internally.
- Omit `t.Helper()` from test helpers — failures will point inside the helper, not the call site.
- Use `time.Sleep` in tests.
- Test unexported functions as a primary strategy — test the exported API. If the exported API is correct, the internals are correct by definition.
- Use `runtime.Caller` or `os.Getwd` to compute absolute fixture paths.
- Use `t.SetEnv` — it disables `t.Parallel()`.
- Hardcode ports, paths, or timeouts as unoverridable constants in production code.
- Define interfaces in the implementing package and export them for callers to use — define them at the point of use.
- Create interfaces wider than the function actually needs.
- Use `init()` to set global state — `init()` runs unconditionally and its side effects cannot be suppressed or reset by tests.
- Use `sync.Once` to initialize global singletons that tests need to reset — they become impossible to reinitialize.

### Table-Driven Tests

Always use table-driven tests, even for a single case:

```go
cases := map[string]struct {
	Input    string
	Expected string
}{
	"empty":   {"", ""},
	"basic":   {"hello", "HELLO"},
	"unicode": {"café", "CAFÉ"},
}
for name, tc := range cases {
	t.Run(name, func(t *testing.T) {
		got := process(tc.Input)
		if got != tc.Expected {
			t.Errorf("expected %q, got %q", tc.Expected, got)
		}
	})
}
```

Use `t.Run` so `defer` works correctly within each case and cases are individually targetable:
`go test -run TestProcess/basic`. Never rely on array indices in failure output.

### Repeat Yourself in Tests

**Prefer a long, flat, self-contained test over a short test that delegates to many helpers.**
When a test fails months later, the reader should understand it by reading one function without
jumping across files. Copy an existing test and modify the relevant lines when writing a similar
new one — abstract into a helper only when it is truly universal (like `testTempFile`).

### Test Assertion Helpers

Define three simple helpers per package. Do not import an assertion library.

```go
func assert(t *testing.T, condition bool, msg string) {
	t.Helper()
	if !condition {
		t.Fatal(msg)
	}
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func equals(t *testing.T, exp, act interface{}) {
	t.Helper()
	if exp != act {
		t.Fatalf("expected %v, got %v", exp, act)
	}
}
```

### Test Helpers

<!-- mdformat off -->

```go
func testTempFile(t *testing.T) (string, func()) {
    t.Helper()
    tf, err := os.CreateTemp("", "test")
    if err != nil { t.Fatalf("testTempFile: %s", err) }
    tf.Close()
    return tf.Name(), func() { os.Remove(tf.Name()) }
}

path, cleanup := testTempFile(t)
defer cleanup()
```

<!-- mdformat on -->

Always call `t.Helper()`. Never return errors — call `t.Fatalf` internally. Return a `func()`
for cleanup; the closure captures `t` so it can also call `t.Fatalf` if cleanup fails.

### Test-Specific Helper Types

Wrap real types to handle setup and teardown:

```go
type TestDB struct{ *sqlite.DB }

func MustOpenDB(t *testing.T) *TestDB {
	t.Helper()
	db := &sqlite.DB{DSN: ":memory:"}
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &TestDB{db}
}
```

### Inline Interfaces in Tests

The caller — not the callee — defines the interface. Declare only the methods the test needs:

```go
type fakeMailer struct {
	SendFunc func(to, subject, body string) error
}

func (f *fakeMailer) Send(to, subject, body string) error {
	return f.SendFunc(to, subject, body)
}
```

### Test Fixtures and Golden Files

<!-- mdformat off -->

```go
// Relative path — go test always sets cwd to the package directory
data := filepath.Join("test-fixtures", "valid_config.hcl")

var update = flag.Bool("update", false, "update golden files")

func TestFormat(t *testing.T) {
    for _, tc := range cases {
        t.Run(tc.Name, func(t *testing.T) {
            actual := Format(input)
            golden := filepath.Join("test-fixtures", tc.Name+".golden")
            if *update { os.WriteFile(golden, actual, 0644) }
            expected, _ := os.ReadFile(golden)
            if !bytes.Equal(actual, expected) {
                t.Errorf("output mismatch\ngot:\n%s\nwant:\n%s", actual, expected)
            }
        })
    }
}
```

<!-- mdformat on -->

Run `go test -update`, review the golden files by eye, then commit them.

### Comparing Complex Structs

<!-- mdformat off -->

```go
// reflect.DeepEqual: correct but poor failure output
if !reflect.DeepEqual(got, want) { t.Errorf("got %+v, want %+v", got, want) }

// Diff library: human-readable diff on mismatch — preferred for non-trivial structs

// testString() pattern: for very large/deeply nested structures
func (g *Graph) testString() string {
    var buf bytes.Buffer
    for _, node := range g.nodes { fmt.Fprintf(&buf, "%s -> %v\n", node.Name, node.Deps) }
    return buf.String()
}
if got.testString() != want.testString() {
    t.Errorf("mismatch\ngot:\n%s\nwant:\n%s", got.testString(), want.testString())
}
```

<!-- mdformat on -->

### Testing Pure Functions

Pure functions need no test doubles. The test has one shape: construct input → call → assert.
Use property-based tests (`testing/quick` or `pgregory.net/rapid`) for functions with clear
invariants:

- **Monotonic/ordered** — output elements satisfy an ordering relation for all adjacent pairs.
- **Idempotent** — applying the operation twice produces the same result as once.
- **Round-trip** — encode then decode recovers the original value.
- **Boundary-preserving** — aggregate properties (min, max, count) are preserved across transformation.
- **Commutative** — reordering inputs produces the same result.

When mocks seem necessary for a domain function, the function is not pure. Make it accept the
dependency as a parameter — the mock disappears.

### Mocks

Hand-write mocks in a `mock` package. No third-party mock generation tools.

```go
type UserService struct {
	FindUserByIDFn      func(ctx context.Context, id int) (*myapp.User, error)
	FindUserByIDInvoked bool
	CreateUserFn        func(ctx context.Context, user *myapp.User) error
	CreateUserInvoked   bool
}

func (s *UserService) FindUserByID(ctx context.Context, id int) (*myapp.User, error) {
	s.FindUserByIDInvoked = true
	return s.FindUserByIDFn(ctx, id)
}
func (s *UserService) CreateUser(ctx context.Context, user *myapp.User) error {
	s.CreateUserInvoked = true
	return s.CreateUserFn(ctx, user)
}
```

`Invoked` booleans let tests assert that a method was or was not called. Use mocks at
interaction boundaries to force explicit specification before implementing — they are a design
tool, not a coverage tool.

### Interfaces as Test Seams

Keep interfaces as narrow as the function requires:

```go
// Too broad — mock must implement all of net.Conn
func ServeConn(c net.Conn) error

// Better — mock only needs io.ReadWriteCloser
func ServeConn(c io.ReadWriteCloser) error
```

Use `internal/` packages to create test boundaries without committing to a public API.

### Networking in Tests

Make real network connections. Never mock `net.Conn`.

```go
func testConn(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0") // OS assigns port
	if err != nil {
		t.Fatalf("testConn: %s", err)
	}
	var srv net.Conn
	go func() { defer ln.Close(); srv, _ = ln.Accept() }()
	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("testConn: %s", err)
	}
	return cli, srv
}
```

### Subprocess Testing

**Option 1 — real binary with availability guard:**

```go
var testHasGit bool

func init() {
	if _, err := exec.LookPath("git"); err == nil {
		testHasGit = true
	}
}
func TestGitStatus(t *testing.T) {
	if !testHasGit {
		t.Skip("git not found")
	}
}
```

**Option 2 — helperProcess mock:**

```go
func helperProcess(s ...string) *exec.Cmd {
	cs := append([]string{"-test.run=TestHelperProcess", "--"}, s...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append([]string{"GO_WANT_HELPER_PROCESS=1"}, os.Environ()...)
	return cmd
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	switch args[0] {
	case "status":
		fmt.Println("nothing to commit")
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(1)
	}
}
```

`TestHelperProcess` is a no-op in normal test runs. Always use `exec.LookPath`; never
hardcode binary paths.

### Public Testing API

For packages consumed by other packages, export test helpers in a `testing.go` file (not
`_test.go`) — compiled into the package and importable by consumers. Use
`github.com/mitchellh/go-testing-interface` instead of `*testing.T` to avoid injecting
`go test` flags into every consumer binary.

```go
// testing.go
import testing "github.com/mitchellh/go-testing-interface"

// TestConfig returns a valid configuration suitable for use in tests.
func TestConfig(t testing.T) *Config {
	t.Helper()
	return &Config{Addr: "127.0.0.1:0", Timeout: 100 * time.Millisecond}
}

// TestConfigInvalid returns a configuration guaranteed to fail validation.
func TestConfigInvalid(t testing.T) *Config {
	t.Helper()
	return &Config{} // missing required fields
}

// TestServer starts an in-memory server and returns its address and a closer.
func TestServer(t testing.T) (net.Addr, io.Closer) {
	t.Helper()
	srv := newInMemoryServer()
	if err := srv.Start(); err != nil {
		t.Fatalf("TestServer: %s", err)
	}
	return srv.Addr(), srv
}
```

Also export mock structs for interfaces the package defines, so consumers can record and replay
calls in their own tests without rebuilding scaffolding.

### Configurable Production Code

Over-parameterize structs so tests can override ports, paths, and timeouts:

```go
const defaultPort = 8080

type ServerOpts struct {
	Port         int // defaults to defaultPort in constructor
	CachePath    string
	testSkipAuth bool // unexported; test-only bypass
}
```

### t.Parallel() — Context Determines the Rule

Hashimoto: avoid `t.Parallel()` — parallel tests make failures ambiguous when tests share global state; you cannot tell whether a failure is a logic bug or a race condition.

Ryer: use `t.Parallel()` freely — when using the `run()` pattern with no global state, concurrent test instances do not interfere.

**Resolution:** Avoid `t.Parallel()` when tests share any global state or package-level variables. Use it freely when each test creates its own fully isolated environment (e.g., calling `run()` with injected dependencies and an OS-assigned port).

### Acceptance Tests

Guard expensive tests with a flag and run them via `go test`:

```go
var flagAcceptance = flag.Bool("acceptance", false, "run acceptance tests")

func TestProvider_basic(t *testing.T) {
	if !*flagAcceptance {
		t.Skip("skipping; run with -acceptance")
	}
	// provision real resources, make real API calls
}
```

### Async Tests

```go
var timeMultiplier = time.Duration(1) // CI can increase this

select {
case <-done:
case <-time.After(5 * time.Second * timeMultiplier):
	t.Fatal("timed out")
}
```

Never use `time.Sleep` with a fixed duration.

### End-to-End Testing for `run`-Based Services

```go
func TestSomething(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go run(ctx, testArgs, testGetenv, nil, io.Discard, io.Discard)
	if err := waitForReady(ctx, 5*time.Second, "http://localhost:PORT/healthz"); err != nil {
		t.Fatal(err)
	}
	// exercise the API as a real HTTP client
}

func waitForReady(ctx context.Context, timeout time.Duration, endpoint string) error {
	client := http.Client{}
	start := time.Now()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if time.Since(start) >= timeout {
				return fmt.Errorf("timeout waiting for endpoint")
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}
```

Delete unit tests that assert the same thing as an end-to-end test. One authoritative set per behavior.

### Inline Types in Tests for Storytelling

When a handler's request/response types are scoped inside the maker func, use minimal inline
structs in tests that express only what the test cares about:

```go
person := struct {
	Name string `json:"name"`
}{Name: "Mat Ryer"}
```

### Legacy Code

1. Identify the specific change — do not set out to test everything.
2. Apply dependency-breaking techniques to open a minimal test seam.
3. Write a **characterization test**: call with an arbitrary input, capture actual output, use that as the expected value. It asserts *current* behavior, not *correct* behavior.
4. Make the targeted change.
5. Run the test — any behavioral difference is now visible.

When a function is too entangled to open a seam: write new behavior as a new, standalone, fully-tested function (**sprout method**), wire it into the call site, and do not add tests to the old code.

______________________________________________________________________

## 11. Concurrency

- Source: *(Bernhardt, Kleppmann)*

**Do:**

- Pass immutable values between goroutines via channels. Immutability makes the channel the only coordination point — no locks required.
- Designate a single goroutine as the sole owner of each external resource. Others produce immutable values and send them; the owner goroutine writes.
- Use `errgroup` or a fan-in channel to collect immutable results from parallel goroutines, then process them in the owner goroutine.
- Inject time as an interface (`type Clock interface { Now() time.Time }`) rather than calling `time.Now()` in domain logic.
- Use a monotonic clock (`time.Since`, `time.Now().Add`) for elapsed time and timeouts. Use wall-clock time only for displaying timestamps to humans.
- Inject random sources (`rand.Source`, `io.Reader` for crypto) rather than global generators.
- Inject I/O as interfaces (`io.Reader`, `io.Writer`, `fs.FS`) rather than accessing filesystem or network directly.

**Do not:**

- Share mutable state across goroutines. If two goroutines need to update the same thing, one is in the wrong place.
- Reach for a mutex as the first response to a concurrency problem — ask whether the data structure can be immutable and passed by value instead.
- Compare wall-clock readings across machines to determine event ordering. Clocks drift; NTP can move time backward.
- Call `time.Now()`, `os.Getenv`, or `rand.Float64()` inside domain logic — push these to the boundary and inject the results.

**Red flag — hidden time dependency:** a test that passes sometimes and fails others, or requires `time.Sleep`, almost always contains a hidden call to `time.Now()`.

______________________________________________________________________

## 12. Distributed Systems and Data Access

- Source: *(Kleppmann)*

### Data Model

- Use **relational** (SQL) for many-to-many relationships, frequent joins, or stable schemas.
- Use **document** only when data has a natural tree structure and the whole document is loaded at once. Do not use document models when many-to-many relationships are common — application-code joins are worse than database joins.
- Use **graph** when anything can be related to anything and the number of connections matters.
- Normalize (use foreign keys) when the same logical value appears in multiple records. Denormalize only after measuring that join overhead is the bottleneck, and only for data that changes rarely.
- Prefer declarative query languages (SQL). They let the engine choose the execution plan; imperative queries hard-code it.
- Do not use the N+1 query pattern. Use JOINs, `IN` clause batch fetches, or eager loading.
- Use cursor-based pagination; avoid offset-based.
- Do not use a relational database as a message queue — use Kafka, RabbitMQ, or SQS.
- Do not couple services via a shared database. Schema changes require coordinated deployment of every consumer.

### Storage Engine Selection

- Use **B-tree engines** (PostgreSQL, MySQL, SQLite, LMDB) for read-heavy or mixed workloads with strong ACID requirements. B-trees offer predictable read performance and are the safe default.
- Use **LSM-tree engines** (LevelDB, RocksDB, Cassandra, HBase) for write-heavy workloads. LSM-trees convert random writes into sequential writes via memtable + SSTable merge.
- Do not assume LSM-trees are always faster for writes. Compaction can interfere with foreground write throughput at high percentiles. Write amplification (a single logical write rewritten 10–30× during compaction) can saturate disk I/O under sustained load.
- Do not use OLTP databases for analytics. Use column-oriented storage (Redshift, BigQuery, Parquet) for OLAP queries that scan millions of rows across few columns.
- Use in-memory databases (Redis, Memcached) when you need sub-millisecond latency and the dataset fits in RAM. Set capacity limits and eviction policies before using them as a primary data store — they fail catastrophically if data grows beyond available RAM.

### Indexing

- Index only what you query. Every index adds write overhead and consumes storage.
- **Composite index prefix rule:** a multi-column index on `(A, B, C)` accelerates queries filtering on `A` alone, `A+B`, or `A+B+C` — but **not** on `B` alone or `C` alone. Design indexes to match your most common query predicates, with the highest-selectivity column first.
- Use a **clustered index** (primary key in the leaf) when you almost always access a row via its primary key.
- Use a **covering index** when a query can be answered entirely from the index without touching the main table.

### Reliability

- Make every mutating operation **idempotent**: assign a client-generated idempotency key (UUID); persist it with the result; check it before executing. Persist durably — in-memory deduplication is lost on restart.
- Set explicit timeouts on all network calls. When a timeout fires, assume the operation may or may not have completed.
- Use exponential backoff with random jitter for retries. Linear retries under load cause coordinated retry storms.
- Classify errors as retryable (timeouts, 503) or non-retryable (400, 404) before writing retry logic.
- Implement circuit breakers around external service calls.
- Implement bulkheads (separate goroutine limits or semaphores per upstream) so one slow dependency cannot exhaust all capacity.
- Implement **backpressure**: when a consumer cannot keep up with a producer's rate, signal the producer to slow down rather than buffering unboundedly. Unbounded queues mask overload and cause OOM crashes.
- Be aware of **tail latency amplification** in fan-out requests. If each backend has a p99 latency of 200ms independently, a request that calls 100 backends in parallel will hit that latency on ~63% of requests — even though any single backend shows it only 1% of the time. Mitigations: bound fan-out; hedge requests (issue a small number in parallel and take the first response); set per-backend timeouts so one slow node does not hold everything.
- Guard against **cross-channel timing races**: writing data to storage and then sending a notification (email, push, message queue) via a different channel can cause the consumer to read from a lagging replica and miss the data the notification is about. Options: use linearizable storage, embed the essential data directly in the notification payload, or pass a `min_version`/etag that the consumer must wait for before proceeding.

### Transactions

Know your database's actual isolation level. PostgreSQL defaults to Read Committed. MySQL InnoDB's "Repeatable Read" is actually Snapshot Isolation. Do not infer semantics from the name.

- Use atomic write operations (`UPDATE counter = counter + 1`) instead of read-modify-write cycles where possible.
- Use `SELECT FOR UPDATE` when you must lock rows read during a read-modify-write cycle at sub-serializable isolation. Do not use it as a substitute for thinking about whether serializable isolation is needed — it locks rows but does not prevent all write skew. Lock the minimum number of rows for the minimum duration.
- Use Serializable isolation to prevent write skew. **Snapshot isolation does not prevent write skew.** Write skew: two concurrent transactions each read a condition, each independently decides to write based on it, and together they violate an invariant neither would violate alone.
- Do not hold transactions open across user interaction.
- Implement retry logic for aborted transactions (SSI, CAS).

| Anomaly             | Read Committed | Snapshot Isolation      | Serializable |
| ------------------- | -------------- | ----------------------- | ------------ |
| Dirty read          | ✓              | ✓                       | ✓            |
| Dirty write         | ✓              | ✓                       | ✓            |
| Lost update         | ✗              | ✓ (atomic / FOR UPDATE) | ✓            |
| Non-repeatable read | ✗              | ✓                       | ✓            |
| Write skew          | ✗              | **✗**                   | ✓            |

### Consistency Models

Use the right consistency model for the operation — stronger models cost more.

- **Linearizability** (strongest): every read sees the most recent write as if there were a single copy. Required for leader election, distributed locks, uniqueness constraints, and counters that must not go negative. Has high latency cost — every operation needs a round-trip to the leader or quorum. Do not use it for general user-facing reads.
- **Causal consistency** (weaker): preserves cause-and-effect ordering — a reply appears after the original message, a row update appears after the row was created. Achievable without cross-node coordination.
- **Eventual consistency**: all replicas converge if no new writes occur. Says nothing about how long convergence takes. Design the application to work correctly when replicas diverge by seconds or more.
- **CAP theorem:** during a network partition you must choose between consistency (refuse writes that may become inconsistent) and availability (accept writes that may diverge). You cannot have both.
- **PACELC:** even without a partition, there is a latency-consistency trade-off. Stronger consistency means waiting for cross-replica coordination on every write.

### Replication and Consistency

- Use single-leader replication as the default. Multi-leader introduces write conflicts; leaderless introduces staleness.
- Do not use asynchronous replication for data you cannot afford to lose. If the leader fails after acknowledging a write but before it replicates, that write is permanently lost. Use semi-synchronous replication (at least one follower acknowledged) for critical data.
- Never use last-write-wins (LWW) when data loss is unacceptable — LWW silently discards the losing concurrent write even though it was acknowledged.
- Use fencing tokens with distributed locks. A fencing token is a monotonically increasing number issued each time the lock is granted. Every write to a shared resource must include the current token; the resource rejects writes with a lower token. Without fencing tokens, two nodes can both believe they hold the lock.
- Do not roll your own consensus protocol. Use ZooKeeper, etcd, or Consul (Raft/ZAB) for leader election, distributed locks, and coordinated configuration.
- Implement read-your-own-writes for any data the user just wrote — users should not see their own submitted data disappear.
- Implement monotonic reads (always read from the same replica for a session) to prevent time travel — a user refreshing a page and seeing older data than they saw before.
- Implement consistent prefix reads to prevent causality violations — a user seeing a reply before the original message.
- Use the **outbox pattern** when propagating changes via CDC: write to a dedicated outbox table within the same transaction as the domain change; point CDC at the outbox table, not the internal domain tables. This keeps CDC event schema stable — internal table renames no longer break downstream consumers.

### Partitioning

Partitioning (sharding) is the only way to scale writes and storage beyond a single node. The
partition strategy determines the system's scalability ceiling and query patterns.

- **Partition by key range** when you need efficient range queries (time series, sequential scans). Watch for hot spots: if all writes go to the current time partition, the system is not actually distributed.
- **Partition by hash of key** when you need uniform write distribution and do not need range queries. Hash partitioning destroys range query efficiency entirely.
- Use **consistent hashing** or a fixed number of virtual nodes (vnodes) so that adding or removing a node requires moving only ~1/N of the data, not all of it.
- Partition **secondary indexes by document** (local index) for write performance, accepting scatter-gather reads. Or partition **by term** (global index) for read performance, accepting that writes must update multiple partitions.
- Monitor partition sizes and split hot partitions. Unbalanced partitions eliminate the benefit of distribution.
- Do not assume a hash-partitioned system supports transactions across multiple keys. Cross-partition transactions require distributed coordination (2PC or consensus) with high latency cost.

### Batch and Stream Processing

- Every index, cache, and materialized view encodes a **write path vs. read path trade-off**: updating an index at write time reduces work at read time. Moving computation to the write path lowers read latency but increases write cost and storage. Choose deliberately, not by default.
- Use **batch processing** (Spark, Hive) for high-throughput transformations on bounded datasets — historical data, ETL, report generation. Batch systems retry failed tasks automatically and produce deterministic output.
- Use **stream processing** (Kafka Streams, Flink) for unbounded data requiring low latency — event-driven pipelines, real-time analytics, alerting, materialized view maintenance.
- Choose the message broker model that matches the workload:
  - **Log-based** (Kafka, Kinesis): durable, ordered within a partition, replayable, fan-out to multiple consumer groups. Use when ordering matters, consumers need replay, or the workload involves CDC/event sourcing.
  - **AMQP/task-queue** (RabbitMQ, SQS): message deleted after acknowledgment; parallelism at individual-message level. Use when messages are expensive to process (any worker can pick any message), ordering is unimportant, and replay is not needed.
  - Do not treat log-based brokers as drop-in replacements for task queues without redesigning for partition-level parallelism.
- Configure a **dead letter queue (DLQ)** on every production consumer. A malformed message causes crash → redeliver → crash → loop, blocking all subsequent messages on an ordered partition. After a configurable number of retries (3–5), route to the DLQ and continue. Monitor DLQs: any message there is an incident requiring investigation.
- Do not query a remote database inside a stream processor's hot path — this is slow and risks overloading the database. Instead, subscribe to a CDC stream of that table and maintain a local read-only copy inside the stream processor (stream-table join via local state store).
- Use the **log-structured approach** (append-only event log) as the source of truth. Derive all other views from the log. This enables recomputing any derived state from scratch.
- Use **event time** (when the event occurred) rather than **processing time** (when it arrived) for windowed aggregations. Late-arriving events are common; processing-time windows produce incorrect results for out-of-order data.
- Use **watermarks** to decide when a window is complete enough to emit results. A watermark is an estimate of how far behind the slowest event is — setting it too tight drops late events; too loose increases output latency.
- Know the three stream join types:
  - *Stream-table join* (enrichment): look up a database record for each stream event. The table is queried at event-processing time; stale table data produces stale enrichment.
  - *Stream-stream join* (windowed): match events from two streams within a time window. Both streams must be buffered in state; state grows proportionally to the window size.
  - *Table-table join* (materialized view): recompute when either source table changes.
- Implement **idempotent stream consumers** — processing the same message twice must produce the same result. Store processed message IDs persistently; in-memory deduplication is lost on restart.
- Prefer **Kappa architecture** (stream layer only, reprocessing via replay) over Lambda architecture (separate batch + speed layers). Lambda requires maintaining two code paths for the same logic.
- Do not use micro-batch streaming (Spark Streaming) when sub-second latency is required.

### Caching

- Do not use a cache as the sole copy of data. The source of truth must exist independently; when the cache is empty, the system must repopulate it from the source, not fail.
- Set an explicit TTL or invalidation strategy for every cached value. Unbounded TTLs mean stale data persists indefinitely.
- Prevent cache stampede: when a popular item expires, many requests miss simultaneously. Use probabilistic early expiration, request coalescing (one rebuilds; others wait), or a background refresh pattern.
- Propagate trace IDs through caches so that cache misses and their downstream database hits are visible in distributed traces.

### Derived Data and CDC

- Do not write to two systems simultaneously (dual write) — use change data capture (CDC). See the outbox pattern in "Replication and consistency" for how to implement CDC without dual-write races.
- Treat the event log as the source of truth. Caches, search indexes, and analytics tables are derived views that can be rebuilt by replaying the log.
- Design CDC consumers to be idempotent — the log may replay events on consumer restart.
- Generate a unique **request ID** for every user-initiated operation and propagate it through every service call and database write. Store it alongside the operation result in the same transaction. On client retry, look up the ID and return the stored result rather than re-executing. A `UNIQUE` constraint on `request_id` is sufficient: a duplicate insert fails and the transaction aborts.
- Separate **notification of success** from **execution of the operation**. Inform the user synchronously ("your payment is processing"); complete side effects (email, webhook, ledger update) asynchronously after all consistency checks pass.
- Use the saga pattern (compensating transactions) instead of 2PC for long-running multi-service operations. Each step is a local transaction; on failure, idempotent compensating transactions undo prior steps. Intermediate states are visible to concurrent transactions — design for that.
- Do not implement business logic in stored procedures. They are hard to test, version, and debug. Keep stateless logic in application servers; keep only state management in the database.

### Timeliness Vs. Integrity

"Consistency" conflates two distinct properties. Confusing them leads to over-engineering (paying for timeliness you don't need) or under-engineering (missing integrity guarantees that matter).

- **Timeliness**: users observe up-to-date state. Violations are temporary — waiting and retrying resolves them. The CAP theorem's "C" and linearizability are timeliness properties.
- **Integrity**: no data is lost, doubled, or corrupted. Violations are permanent and require explicit detection and repair. ACID atomicity and durability are integrity properties.
- Prioritize integrity over timeliness. A statement that takes 24 hours to reflect a transaction is annoying (timeliness violation). A statement where the sum of transactions does not equal the balance is catastrophic (integrity violation).
- Event-driven / stream-based systems naturally sacrifice timeliness (reads may be stale) while preserving integrity (exactly-once delivery, idempotent consumers). This is usually the correct trade-off.
- **Compensating transactions** ("apology workflows") are a valid pattern when the cost of occasionally violating a constraint is low and recoverable — refund, upgrade, apology email. Accept the write optimistically and check the constraint after the fact. Airlines, hotels, and banks operate this way deliberately. Do not use strict synchronous coordination (2PC, distributed locks) for constraints the business already handles with apologies.

### Durable Execution

When a business process spans multiple services and each step must execute exactly once despite failures, durable execution eliminates the need to hand-roll retry and idempotency logic across the entire call graph.

- Use a **durable execution framework** (Temporal, Restate) for workflows that involve multiple external service calls (payment gateways, email providers, third-party APIs) where partial execution is unacceptable — credit card charged but bank account not credited.
- Durable execution works by logging every RPC call and its result to durable storage. On failure and re-execution, the framework replays the log, skipping already-completed steps and returning their cached results.
- Write workflow code to be **deterministic**: same inputs must produce the same sequence of calls in the same order. Do not use `time.Now()`, `rand`, or any non-deterministic call inside workflow code — use the framework's own deterministic wrappers.
- Do not reorder or add/remove function calls in an existing workflow definition that may have in-flight executions. The framework replays old executions using the current code; reordering breaks them. Deploy new workflow logic as a new workflow version and let old executions drain.
- External services called from workflows must still expose **idempotent APIs** with unique request IDs — durable execution re-executes tasks on failure and a non-idempotent external service will still produce duplicate actions.

### Encoding

- Use binary encoding (Protocol Buffers, Thrift, Avro) over JSON/XML for internal inter-service communication or long-term storage. JSON is acceptable for external-facing REST APIs.
- Never use language-native serialization (Python pickle, Java Serializable) across process boundaries — language-locked and have known deserialization exploits.
- Maintain backward compatibility (new code reads old data) AND forward compatibility (old code reads new data) for data that outlives a single deployment.
- Do not remove a field from a Protocol Buffers schema without marking it `reserved`.

______________________________________________________________________

## 13. Naming, Comments, and Code Clarity

- Source: *(Ousterhout, Feathers)*

### Names

- Use names specific enough that a reader at the call site, without documentation, can correctly guess the meaning: `getActiveIndexlets()` not `getCount()`.
- Make boolean names predicates: `cursorVisible` not `blinkStatus`.
- Use a consistent name for a given concept everywhere it appears — do not invent synonyms.
- Do not use the same name or pattern for dissimilar things. Consistency depends on readers trusting that similar appearance means similar behavior.
- Use distinguishing prefixes when similar names exist in the same scope: `srcFileBlock` vs. `dstFileBlock`.
- Treat difficulty naming something as a design smell: unclear purpose, or doing too many things.

### Comments

- Write a godoc comment for every exported type (overall abstraction and limitations), every non-trivial variable (units, invariants, null semantics, ownership), and every exported method (behavior, arguments, return values, side effects, error conditions, preconditions).
- Write interface comments **before** writing the body. If the comment is long and tangled, the interface is wrong.
- Write implementation comments that explain **why** a non-obvious approach was chosen, not **what** the code is doing.
- Do not repeat the code in a comment — if the comment can be reconstructed from the adjacent code, it adds nothing. Do not use the same words in the comment that appear in the method name.
- Put design rationale in the code where developers will see it during changes — not only in commit messages.
- Document cross-module design decisions in a single authoritative location; reference it from the affected code.
- Document event handlers and callbacks with a comment stating exactly when they are invoked, since the control flow cannot be traced statically.
- Think of good comments and clean interfaces as design work, not busywork. They reveal design problems early when they are cheapest to fix.

### Making Code Obvious

Code is obvious when a reader's first guess about its behavior is correct.

- Use white space to separate the major phases of a method. Pair each block with a short comment describing its purpose — not what the code does, but what phase of the work it represents.
- Define specialized structs or named types for values that belong together instead of generic pairs or tuples whose element names convey nothing.
- Do not violate common reader expectations without documenting the deviation prominently (e.g., a constructor that spawns background threads, a method that modifies a parameter).
- Match declaration types to allocation types when the concrete type matters to readers.

______________________________________________________________________

## 14. Observability

- Source: *(Kleppmann)*
- Instrument every service with request rate, error rate, and latency at p50/p95/p99 (RED: Rate, Errors, Duration).
- Emit structured logs (JSON). Include: timestamp (ISO 8601), service name, log level, correlation/trace ID, operation name, duration, error type.
- Include a correlation ID in every log line and every downstream request.
- Expose a **liveness** endpoint (process is alive) and a **readiness** endpoint (migrations complete, caches warm, downstream connections established).
- Use distributed tracing (OpenTelemetry, Jaeger, Zipkin) for systems with more than two services.
- Use percentile latency (p95, p99) for SLOs. Tail latencies govern user experience; averages hide them.
- Track data freshness for batch and stream pipelines — a stale aggregation table needs an alert, not just a log message.
- Monitoring is a first-class feature. The absence of a metric that should be incrementing is itself a signal of failure.

______________________________________________________________________

## 15. Design Philosophy

- Source: *(Ousterhout, Feathers)*
- **Invest in the design.** The right design, even when it takes 10–20% longer, pays back within months. Consider at least two radically different designs before committing.
- **Do not let complexity accumulate.** A single hack matters little; hundreds accumulate into a system that cannot be changed. Every tactical shortcut increases complexity a little.
- **Improve the design whenever you touch code.** After a change, the system should look as if it had been designed with that change in mind from the start.
- **Delete dead code.** Every line is implicitly a claim it is in active use. Dead code makes that claim falsely. Recover from version control if needed — the rewrite will be better.
- **Have the carrying cost conversation.** When a stakeholder requests a new feature, surface any existing feature whose presence makes the new one significantly harder to add. "How much revenue does that feature actually produce?" is a legitimate engineering question that is often never asked.
- **Give explicit standing permission for targeted rewrites.** If a module is too hard to refactor, the functionality is replaceable, and the interface is clear — rewrite it at bounded, module-sized scale without requiring approval. Do not pursue large-scale rewrites that must simultaneously match every feature of the existing system.
- **Make code obvious.** Code is obvious when a reader's first guess about its behavior is correct. Obscurity is a direct cause of complexity.
- **Be consistent.** Follow naming, style, and structural conventions already present in the codebase. Enforce them with linters, not human vigilance. Do not introduce variant patterns for personal preference.
- **Design for performance without adding complexity.** Simplicity and performance are mostly aligned: simple code has fewer layers, fewer conditions, and better cache behavior.
  - Choose naturally efficient algorithms as a first choice when they are as simple as slower alternatives.
  - Identify the critical path and minimize the work on it. Remove special-case checks from the hot path; handle edge cases with a single early branch that returns before the fast path.
  - Do not optimize prematurely. Measure first. Discard performance changes that do not produce a measurable improvement — they add complexity for no gain.

______________________________________________________________________

## 16. Integration Patterns

- Source: *(Hohpe & Woolf)*

### Choose the Integration Style Before Designing the Interface

The four integration styles produce fundamentally different coupling profiles. Choosing late usually means choosing RPC, which looks simplest and ages worst.

| Style           | Best for                                                              | Cost                                                   |
| --------------- | --------------------------------------------------------------------- | ------------------------------------------------------ |
| Messaging       | Independent availability, retry, load distribution, traffic buffering | Higher complexity; requires broker                     |
| RPC             | Synchronous answer needed before caller can proceed                   | Tightly couples caller and callee lifetimes            |
| Shared Database | Multiple systems needing the same transaction                         | Every consumer depends on the schema — hidden coupling |
| File Transfer   | Bulk batch exchange where latency is not critical                     | High latency; missed transfer requires manual recovery |

Two properties make messaging work: **send and forget** (sender hands message to channel without waiting for receiver) and **store and forward** (channel persists messages until receiver is available). Without both, you have asynchronous-looking RPC.

**Do not** default to RPC because it "looks like a function call". The caller blocks, the call fails atomically with the callee, and retry must be added by hand.

> **Red flag — Accidental synchrony**: an asynchronous system reimplemented as a sequence of blocking calls. You paid the cost of messaging and got the coupling of RPC.

### Coupling Is Multi-Dimensional

Coupling has eight distinct dimensions. A system can be loosely coupled on one and tightly coupled on another — the combination determines actual fragility:

| Dimension    | Change that propagates                                  | Mitigation                                                                  |
| ------------ | ------------------------------------------------------- | --------------------------------------------------------------------------- |
| Technology   | Language / framework / library version                  | Standard wire formats (JSON, Protobuf)                                      |
| Location     | Recipient moves to a new address or scales out          | Logical channel names instead of IP / hostname                              |
| Topology     | Intermediary inserted; recipient added or removed       | Topology-decoupled channels; intermediary insertion without changing sender |
| Data format  | Field renamed, repositioned, added, or removed          | Tagged formats (JSON, XML); Message Translator at the boundary              |
| Semantic     | Agreed field name but not its meaning                   | Canonical Data Model; explicit documented contracts                         |
| Conversation | Message order, retry rules, or timeout behavior assumed | Explicit protocol; Idempotent Receiver                                      |
| Order        | Receiver assumes messages arrive in sequence            | Design for out-of-order tolerance; Resequencer only when required           |
| Temporal     | Slow / unavailable provider blocks requester            | Asynchronous messaging; graceful degradation                                |

- **Do** identify which coupling dimensions are most likely to cause a production incident before choosing the integration style. A coupling analysis is more useful than a blanket "loosely coupled" claim.
- **Do** prefer logical addressing. Location-coupling spectrum from tight to loose: `Hard-coded addresses → Host Names / URLs → Logical Names → Topics → Content filtering → Explicit Composition`. Moving toward the loose end enables **composability** — an intermediary (translator, tap, load balancer) can be inserted without changing the sender. The extreme end shifts all change dependency to an assembler that becomes a maintenance bottleneck.
- The appropriate level of coupling depends on the level of control over the endpoints. When you own all components and have full automation, some coupling is acceptable.
- **Warning — Hidden coupling**: a topology-decoupled serverless application whose event payload is source-specific forces all consumers to change when the source changes — topological decoupling voided by data-format coupling. Name the remaining coupling dimension explicitly; do not declare a system "loosely coupled" because one dimension improved.

### Keep Application Code Unaware of Messaging

Business logic must not know about channels, message headers, broker APIs, or delivery semantics.

- **Do** wrap all outbound messaging behind a **Messaging Gateway** — an interface whose methods express domain intent (`SubmitOrder(ctx, order)`) not messaging primitives (`Publish(ctx, channel, body)`). The real implementation uses the broker; the test implementation invokes callbacks synchronously with no broker required.
- **Do** handle all inbound messages through a **Service Activator** — a thin adapter that reads a message from a channel and calls domain logic. Domain logic does not know it was invoked via a message.
- **Do not** let message headers, correlation IDs, or broker-specific concepts leak into domain objects.

### Channel and Message Patterns

**Channel types:**

- **Point-to-Point**: each message consumed by exactly one receiver. Use for commands, work items, and load-balanced processing (Competing Consumers).
- **Publish-Subscribe**: each message delivered to all subscribers. Use when multiple independent consumers need the same event.
- **Dead Letter**: receives messages that fail processing after all retries. Configure on every production consumer — a failing message in an ordered partition causes crash → redeliver → crash loops.
- **Guaranteed Delivery**: persists messages until acknowledged. Without this, broker restarts lose messages.

**Message construction:**

- Include a globally unique **Message ID** and a **Correlation ID** (echoed from the requesting message) in every message. These are prerequisites for deduplication and request-reply correlation.
- Set a **TTL / Expiration** on time-sensitive messages. Stale messages with no TTL accumulate in queues and may be processed long after they are relevant.
- Use a **Return Address** header for request-reply — the channel on which the receiver should send its response.

**Message routing:**

- **Content-Based Router**: routes each message to a channel based on message content. The router knows all consumers — central knowledge required.
- **Message Filter**: each consumer subscribes with filter criteria; only matching messages are delivered. The consumer controls what it receives — no central knowledge required.
- **Splitter → Aggregator**: split one message into N sub-messages for parallel processing; collect N responses into one reply. Track correlation and handle timeouts for partial results.
- **Process Manager (Saga)**: for long-running multi-step workflows where each step sends a message. The Process Manager tracks completed steps and what comes next. Required when the workflow cannot complete in one transaction or fit in memory.

**Message transformation:**

- **Message Translator**: adapts message formats at the boundary between systems. Never let format differences propagate into shared domain logic.
- **Canonical Data Model**: one neutral schema shared by all systems; translators convert to/from it at the boundary. Reduces N×(N−1) translator pairs to 2×N at the cost of maintaining the neutral schema.

**Idempotency:**

- **Idempotent Receiver**: every consumer must tolerate receiving the same message more than once. Store a processed message ID persistently before executing. In-memory deduplication is lost on restart.

### Event-Driven Architectures and Coupling

- Events are messages; the word "event" describes message semantics, not the interaction style.
- **Most EDA decoupling derives from Publish-Subscribe channels, not event semantics.** A command message on a Pub/Sub channel gains the same topological benefits.
- **Pub/Sub coupling is asymmetric**: adding a subscriber does not require changing the publisher. Most valuable when you do not control the publisher.
- **Recipient List ≠ Pub/Sub**: AWS EventBridge rules and targets act as a Recipient List — adding a target requires changing a central element. It negates the "subscriber-side freedom" benefit while maintaining the "publisher-side stability" benefit.
- The big coupling improvement is RPC → messaging (temporal decoupling). The step from messaging to Pub/Sub messaging is smaller.
- Adding recipients easily makes modifying the system harder: every recipient depends on the event schema. Taking advantage of topology freedom exposes data-format coupling.

> Scattered implicit dependencies are not loose coupling. EDA proponents are often early in the lifecycle; they will not live with the consequences of their coupling decisions.

### Control Flow: Push, Pull, Queues, and Drivers

Control flow describes which element actively drives the interaction — independently of which direction data flows.

**Atomic roles:**

| Element     | Behavior                                                                             |
| ----------- | ------------------------------------------------------------------------------------ |
| **Sender**  | Actively pushes data; data and control flow align (left→right)                       |
| **Sink**    | Passively receives from a Sender                                                     |
| **Source**  | Passively provides data; waits to be fetched                                         |
| **Fetcher** | Actively requests data from a Source; data and control flow face opposite directions |

**Pipeline combinations:**

| Element    | Behavior                                                                                                                   |
| ---------- | -------------------------------------------------------------------------------------------------------------------------- |
| **Queue**  | Connects a Sender to a Fetcher; inverts control flow; decouples arrival and departure rates                                |
| **Driver** | Actively fetches from a Source and actively pushes to a Sink; inverts control flow like a Queue but controls fetch cadence |
| **Pusher** | Receives from a Sender; pushes processed results to next element; synchronous (each send coincides with a receive)         |
| **Puller** | Fetches from a Source; provides results when a downstream Fetcher requests; synchronous in the same sense                  |

**Design rules:**

- A Sender and a Fetcher facing each other need a **Queue** to connect. A Source and a Sink need a **Driver**.
- A **Driver** rate-limits by adjusting fetch cadence without buffering — when a target is slow, the Driver slows fetching rather than building a backlog. AWS EventBridge Pipes acts as a Driver, which is why it maintains message order and can rate-limit without an explicit queue.
- **A queue inverts control flow.** This enables traffic shaping — a queue converts a spiky arrival rate into a steady departure rate. Synchronous systems collapse past a load threshold as resources are consumed accepting new requests rather than processing existing ones.
- Cloud services that appear to push messages (SNS, EventBridge Bus) have internal queues and driver pools. They optimize for throughput, not latency — P90 latency can be 250–500ms.
- Pull delivery can outperform push despite polling overhead — the receiver controls batch size and polling rate.

### Queue Flow Control

Queues have finite capacity. Unlimited queues mask overload and cause OOM crashes, long wait times, or stale processing.

- **Little's Law**: average wait time = queue depth ÷ arrival rate. Long queues increase wait time even when the system looks healthy.
- Three reactive flow control mechanisms:
  - **TTL (Time-to-Live)**: drop old messages to make room for new ones. Use when message value decays rapidly (data streams, time-sensitive orders).
  - **Tail drop**: reject new arrivals when the queue is full. Use when existing messages are too valuable to drop; senders need a retry/feedback mechanism.
  - **Backpressure**: signal upstream systems to reduce the arrival rate (e.g., return HTTP 429; show a "too busy" message). Use when senders can adapt.
- **Rate limiting (proactive)**: configure a maximum delivery rate before flow control ever engages. Preferable to reactive mechanisms. A Driver implements this by adjusting fetch cadence.
- Monitor **queue message age**, not just depth. Depth zero with workers processing 24-hour-old messages is a failure state.

> **Red flag — All lights green, system is down**: queue depth is zero, workers are active, error rate is low — but users get stale results because queue age is unbounded. The metric you are not watching is the one that matters.

______________________________________________________________________

## 17. Site Reliability Engineering

- Source: *(Beyer, Jones, Petoff, Murphy)*

### The 50% Operational Cap

SRE teams that spend more than half their time on operational work — incidents, tickets, manual toil — have no capacity to eliminate the root causes of that work.

- **Do** track the split between engineering work (projects, automation, capacity planning) and operational work (incidents, tickets, manual tasks) every week.
- **Do** redirect excess pages and tickets to the development team when the SRE team is over the cap. This creates the right incentive for developers to fix reliability at the source rather than externalizing it to SRE.
- **Do** invoke **"give back the pager"** when a service persistently exceeds the cap and structural fixes require multiple quarters. SRE formally returns on-call responsibility to the development team while both teams work together to make the service operationally sustainable. Without this enforcement mechanism, the 50% cap has no teeth.
- **Do not** absorb unbounded operational load silently. A team that accepts every interrupt signals that reliability problems have no cost, guaranteeing more of them.

### SLIs, SLOs, and Error Budgets

- **Service Level Indicator (SLI)**: a specific measurable ratio — successful requests / total requests, latency below threshold, queue depth below limit. Measurable from the user's perspective, not internal instrumentation.
- **Service Level Objective (SLO)**: a target percentage for an SLI over a rolling window (e.g., 99.9% of requests succeed over 30 days). Strict enough to matter; loose enough to allow shipping.
- **Error budget**: `(1 − SLO) × window`. At 99.9% over 30 days = 43.8 minutes. Track consumption in real time.
- **Do not** set SLOs at 100%. 100% eliminates all error budget, halts all risk-taking, and is not achievable.
- **Reliability has sharply diminishing returns** past the point users can observe. An additional nine of availability can cost 100× the previous one. A user on a 99% reliable smartphone cannot distinguish 99.99% from 99.999% service reliability. Error budgets make this trade-off explicit.
- When the error budget is healthy, release aggressively. When exhausted, freeze releases and focus on reliability work. This removes subjective negotiation from the reliability conversation.
- Distinguish the **SLO** (internal target) from the **SLA** (contractual commitment). The SLO must be tighter than the SLA to provide headroom before breaching contractual obligations.

### Alert on Symptoms, Not Causes

Alerting on causes (CPU usage, disk full, process restarts) generates noise and fatigue without tracking what users actually experience.

**Three tiers of monitoring output:**

1. **Alerts** — page the on-call engineer immediately. Must correspond to a user-visible symptom. Every page that fires without requiring immediate action is alert fatigue.
2. **Tickets** — action required within days; filed to a queue and prioritized.
3. **Logs** — forensics only. Never alert from log lines; use metrics for alerting.

**Four golden signals** — the minimum instrumentation for any service:

1. **Latency** — time to serve a request. Distinguish successful from failed requests; failed requests can skew latency fast.
2. **Traffic** — demand on the system (RPS, transactions per second).
3. **Errors** — rate of failed requests: explicit (500s), implicit (wrong content), policy (SLO violations).
4. **Saturation** — how full the system is; predict utilization before capacity is exhausted.

**Multi-window, multi-burn-rate alerting**: raw SLI violation alerting is too noisy (high false positive) or too slow (high false negative). Use burn-rate alerts:

- A burn rate of 1 consumes error budget at exactly the SLO rate.
- Alert at high burn rate (e.g., 14×) over short windows (1h) for fast-burning incidents.
- Alert at lower burn rate (e.g., 2×) over long windows (6h) for slow-burning degradation.

### Eliminate Toil

**Toil** is operational work that is all of: manual, repetitive, automatable, scales O(n) with service growth, and produces no permanent improvement. Toil is distinct from overhead or busywork in general.

- Track toil explicitly. Untracked toil expands to fill all available capacity.
- When a runbook step is repeated more than twice, automate it.
- Do not automate a broken process — fix it first, then automate. Automation of a broken process is reliably wrong.

### On-Call

- **25% cap**: on-call should consume no more than 25% of a single SRE's time (the remaining 25% of the 50% operational cap covers other reactive work).
- Structure: **primary** on-call responds to pages; **secondary** acts as backup and handles overflow. Prevents single points of failure.
- **Follow-the-sun**: rotate on-call responsibility across time zones so no team is permanently on night duty.
- Minimum team size for sustainable on-call rotation: **8 engineers** (single-site), **6 engineers** (dual-site with follow-the-sun). Smaller teams produce unsustainable schedules.
- **Operational underload** is also a risk: teams handling fewer than 1–2 significant events per quarter lose incident sharpness. Use Wheel of Misfortune exercises to maintain readiness.
- An alert that fires but requires no immediate action is a ticket, not an alert. Miscategorized pages are toil.

### Postmortems

- Write a **blameless postmortem** for every incident that breaches the SLO or causes significant user impact.
- Focus on systemic causes, not individual errors. Blame inhibits honest reporting and prevents organizational learning.
- Document: timeline, root cause, contributing factors, impact, and action items with owners and due dates.
- Publish postmortems widely. The value is in organizational learning, not the filing.
- Distinguish **trigger** (the action that precipitated the incident) from **cause** (the systemic brittleness that made the action dangerous). 70% of outages are caused by changes to live systems — the cause is the fragility that made the change dangerous, not the change itself.

### Incident Command

- Assign an **Incident Commander (IC)** at the start of any significant incident. The IC coordinates response; they do not debug. Debugging while coordinating is a cognitive overload failure mode.
- Roles: IC, Operations Lead (execution), Communications Lead (status updates to stakeholders), Scribe (logs decisions and actions in real time).
- Keep the incident channel focused: no speculation, no blame, only actionable information.
- Use **Wheel of Misfortune** exercises regularly — roleplay past incidents from postmortems to build muscle memory for response before a real incident.

### Production Readiness Review (PRR)

Before accepting on-call responsibility, SRE performs a PRR to verify:

- SLIs and SLOs are defined and instrumented.
- Monitoring and alerting are in place.
- Runbooks are written and tested.
- Capacity planning is done.
- Load testing results exist.
- Rollback procedure is documented and tested.

A service that cannot pass PRR is not ready for production SRE support. Accepting it anyway absorbs operational burden without the means to improve it. Repeat PRR after significant architectural changes.

### Automation Spectrum

| Level              | Description                                  |
| ------------------ | -------------------------------------------- |
| 0 — Manual         | Human performs the action each time          |
| 1 — Runbook        | Documented steps a human follows             |
| 2 — Recommendation | System identifies the action; human approves |
| 3 — Assisted       | System performs action with human monitoring |
| 4 — Autonomous     | System acts without human involvement        |

- Consistency is a primary automation benefit — automation always follows the same steps.
- Automation that is inconsistent (sometimes runs, sometimes does not) is worse than no automation — it creates the illusion of coverage.
- Deploy changes via staged rollout with automatic rollback on metric degradation. 70% of outages are change-induced; staged deployment reduces blast radius.

### Capacity Planning

- Plan capacity **8–12 quarters ahead** (2–3 years). Procurement timelines are long; shortfalls become incidents.
- Design for **N+2 redundancy**: N instances serve traffic; one buffers maintenance; one buffers unexpected failures.
- Use load testing to identify actual capacity limits before traffic reaches them.
- Revisit capacity plans when traffic patterns change significantly — marketing campaigns, product launches, and regulatory deadlines create demand spikes outside the forecast.

______________________________________________________________________

## 18. CLI Command Patterns

**How to choose:** check `go.mod`.

- `github.com/spf13/cobra` present → **Pattern C: Cobra**
- `github.com/peterbourgon/ff/v4` present → **Pattern B: ff/v4**
- Neither → **Pattern A: stdlib**

Check Cobra first — a project may have both, and Cobra takes precedence because it owns the command tree.

**Shared rules (all patterns):**

| Rule                                                                | Rationale                                                                                                                                                                                                                                          |
| ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Match the framework already in `go.mod`; do not introduce a new one | The spec follows the project, not the other way around                                                                                                                                                                                             |
| No `init()` functions                                               | `init()` runs unconditionally at startup before flag parsing, cannot be suppressed in tests, and has implicit cross-package ordering; register commands explicitly in `cmd/cmd.go` and initialize resources at the call site where they are needed |
| One command per package (or file in the flat Cobra layout)          | Factory function is the only public API                                                                                                                                                                                                            |
| Return `error`; never call `os.Exit` in a command                   | Only `main` controls exit codes                                                                                                                                                                                                                    |
| `package cmd` is a dispatcher, not a binary                         | It is imported by `main`, not executed directly                                                                                                                                                                                                    |
| Dispatch key must be unique across all commands                     | Duplicate keys silently shadow each other                                                                                                                                                                                                          |
| Error strings: lowercase, no trailing punctuation                   | Format: `<command>: <reason>`                                                                                                                                                                                                                      |

### Pattern A: Stdlib

Use when neither `cobra` nor `ff/v4` is in `go.mod`. The `Command` struct lives in
`pkg/pattern/command/base.go` — do not modify it.

- The factory function (`<Name>Command`) is the **only exported symbol** in the package. Logic goes in the unexported `<name>Cmd`.
- Use `flag.NewFlagSet(name, flag.ContinueOnError)` for flags. Never use the global `flag` package.
- `flag.ExitOnError` calls `os.Exit` on parse failure — always use `flag.ContinueOnError`.
- When `fs.Parse` returns `flag.ErrHelp` (user passed `-h`), return it **unwrapped**. `main` catches it with `errors.Is(err, flag.ErrHelp)` and exits 0.
- `flag.ContinueOnError` auto-prints usage to stderr on `-h` or unknown flag — do not add a redundant `fs.Usage()` call.
- The `cmd *command.Command` parameter carries command metadata; ignore it with `_` when unused.
- `cmd/cmd.go` is the only place commands are registered (explicit `commands` slice, no `init()`).

### Pattern B: Ff/v4

Use when `github.com/peterbourgon/ff/v4` is in `go.mod` (and Cobra is not). Each command is a package containing a `Config` struct, an exported `New` factory, and an unexported `exec` method.

- `New` and `Config` are the **only exported symbols** in the package.
- `New` self-registers by appending to `parent.Command.Subcommands` — no separate registration step.
- Flag values are bound to `Config` fields in `New()`, **not** inside `exec`. `exec` reads already-parsed values; never call `Parse` inside `exec`.
- `SetParent(parent.Flags)` **must** be called on every subcommand's flag set so parent flags (e.g. `--verbose`) work at any depth. `SetParent(nil)` is safe.
- Write to `cfg.Stdout` / `cfg.Stderr`; never `os.Stdout` / `os.Stderr`.
- Every behavioural knob must be a registered flag on `cfg.Flags` — never use hard-coded values, package-level variables, or `os.Getenv` outside the flag set; they make settings invisible to `-h` and break the flags-first contract.
- Return `root.ExitError(n)` from `exec` for a controlled non-zero exit without printing `"error: ..."` to stderr. Never call `os.Exit` inside command code; only `main()` calls `os.Exit` — after `stop()` releases the signal context.
- Set `Exec: nil` (omit the field) on group-parent commands. They return `ff.ErrNoExec`; the dispatcher suppresses help output for it and returns the error as-is; `run()` in `main.go` treats it as success.
- Post-parse initialization (API clients, DB connections, loggers) belongs in `cmd/cmd.go` between `r.Command.Parse(args)` and `r.Command.Run(ctx)`. Assign dependencies to fields on `root.Config` so all `exec` functions inherit them via embedding.
- `ff.ErrHelp` and `ff.ErrNoExec` are not failures — `run()` in `main.go` handles both as success via a `switch` on the returned error.
- `root.Config` holds `Stdin io.Reader` alongside `Stdout` and `Stderr`; `root.New` takes `(stdin io.Reader, stdout, stderr io.Writer)`. The dispatcher's `Run` signature is `Run(ctx, args, stdin, stdout, stderr)`; `run()` in `main.go` is the call site — it receives `stdin`, `stdout`, and `stderr` from `main()` and forwards them to `cmd.Run`. In tests, inject `strings.NewReader("")`, `&bytes.Buffer{}` (or `io.Discard`), `&bytes.Buffer{}` (or `io.Discard`) into `run()` rather than calling `cmd.Run` directly.
- `main.go` uses `signal.NotifyContext` for signal-safe shutdown. `run()` returns an `int` exit code; `main()` calls `stop()` then `os.Exit(code)`. If `run()` called `os.Exit` directly, `stop()` would never execute, leaving the signal-handler goroutine running until the process terminated. `run()` is annotated `// run is intentionally separated from main to improve testability. Please preserve this comment.` to prevent it from being inlined into `main` during refactoring.
- The version command (`cmd/version/`) uses `var Version = "dev"` — the one permitted package-level mutable variable in a command package. The Go linker's `-ldflags "-X <pkg>.Version=<val>"` can only override a `var`; a `const` or local variable is link-time immutable. Do not use `init()` to populate it; read `debug.ReadBuildInfo()` inside `exec` on demand.
- The `climax` tool scaffolds new applications (`climax init`) and adds commands (`climax add`). The marker comments `// climax:name`, `// climax:root-pkg`, `// climax:imports`, and `// register new commands here` in `cmd/cmd.go` must not be removed.
- If the application has no shared flags, omit `ff.NewFlagSet` and leave `cfg.Flags` nil; `ff` creates an empty flag set automatically and `--help` still works. This is the default generated by `climax init`; uncomment the `ff.NewFlagSet` line when adding the first shared flag.

### Pattern C: Cobra

Use when `github.com/spf13/cobra` is in `go.mod`. Subcommands are `*cobra.Command` values returned by a `NewCommand` factory.

- `NewCommand` is the **only exported symbol** in the package.
- Never declare `var rootCmd *cobra.Command` at package level. Package-level command variables prevent isolated testing and can cause data races in parallel tests.
- Declare flag-destination variables **inside `NewCommand`** (not at package level) — closures capture them; each `NewCommand()` call gets independent state, safe for parallel tests.
- Bind flags in `NewCommand`, not inside `RunE`. Flags are parsed before `RunE` runs; binding inside `RunE` has no effect.
- Use `RunE`, not `Run`. `Run` cannot return errors.
- Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`; never `os.Stdout` / `os.Stderr`.
- Set `Args` explicitly on every command. The default (`nil`) silently accepts any arguments — equivalent to `ArbitraryArgs`.
- Set `SilenceErrors: true` and `SilenceUsage: true` on the root command. `main` prints the error and controls the exit code; Cobra must not print it first.
- `cmd.Usage()` is for genuine usage errors only (wrong argument count, invalid flag value). Never call it for runtime errors (network, DB, permission denied) — usage output is noise for those failures.
- `PersistentPreRunE` is the hook for post-parse initialization. By default Cobra calls only the **closest ancestor**'s `PersistentPreRunE` — if a subcommand defines its own, the root's hook silently does not run, causing nil-pointer panics. Either avoid `PersistentPreRunE` on subcommands, or set `cobra.EnableTraverseRunHooks = true` before `ExecuteContext`.
- For subcommands in separate packages that need shared state: pass a **getter function** through the constructor (e.g. `func() *api.Client { return sharedClient }`), not the pointer itself. The getter closes over the package-level variable that `PersistentPreRunE` sets; reading the pointer directly at construction time gets the zero value.
- Omit `RunE` on group-parent commands. Cobra prints help and returns nil when a group parent is invoked without a subcommand.
- Use `root.SetArgs(args)` and `root.ExecuteContext(ctx)` (not `Execute()` alone) so that tests can inject args deterministically without the test binary's own args bleeding in.
- Use the **flat layout** (`cmd/*.go`, all in `package cmd`) for simple CLIs. Switch to the **per-package layout** (`cmd/<name>/<name>.go`) when a subcommand has its own test file or significant implementation.
- Hook execution order: `PersistentPreRunE` (closest ancestor) → `PreRunE` → `RunE` → `PostRunE` → `PersistentPostRunE` (closest ancestor).
- Available positional argument validators: `NoArgs`, `ArbitraryArgs`, `ExactArgs(n)`, `MinimumNArgs(n)`, `MaximumNArgs(n)`, `RangeArgs(min, max)`, `OnlyValidArgs` (requires `ValidArgs` slice), `MatchAll(a, b, ...)`. For domain-specific validation, wrap a built-in and add your own check inside an `Args` func literal.
- Flag group constraints: `cmd.MarkFlagsRequiredTogether("a", "b")` (all or none), `cmd.MarkFlagsMutuallyExclusive("a", "b")` (at most one), `cmd.MarkFlagsOneRequired("a", "b")` (at least one). These constraints are enforced automatically before `RunE`.
- **`PersistentPreRunE` does not run in subcommand unit tests.** When a subcommand is constructed and executed without a root parent, the root's hook is absent — any dependencies normally initialized there (API clients, DB connections) will be at their zero value. Either inject them explicitly through the constructor in the test, or use integration tests through `cmd.Execute` when the full initialization chain matters.

### Pattern Comparison

| Concern                               | Pattern A: stdlib                                  | Pattern B: ff/v4                                                                                | Pattern C: Cobra                                                                                                   |
| ------------------------------------- | -------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Detection                             | Neither cobra nor ff/v4 in go.mod                  | ff/v4 in go.mod                                                                                 | cobra in go.mod                                                                                                    |
| Command representation                | `Command` struct with `Run` fn pointer             | `*ff.Command` struct with `Exec` fn field                                                       | `*cobra.Command` struct with `RunE` fn field                                                                       |
| Flag binding                          | `flag.NewFlagSet` inside `<name>Cmd`               | `ff.FlagSet` bound in `New()`                                                                   | `pflag` via `cmd.Flags()` / `cmd.PersistentFlags()`                                                                |
| Flag inheritance                      | Not supported                                      | `SetParent(parent.Flags)`                                                                       | `PersistentFlags()` on any ancestor                                                                                |
| Flag-value state                      | Local vars in `<name>Cmd`                          | Fields on `Config` struct                                                                       | Local vars closed over inside `NewCommand`                                                                         |
| I/O                                   | `fmt.Println` to real stdout                       | `cfg.Stdout` / `cfg.Stderr`                                                                     | `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`                                                                          |
| Context                               | Not threaded                                       | `context.Context` arg in `exec`                                                                 | `cmd.Context()` inside `RunE`                                                                                      |
| Shared dependencies                   | Not supported                                      | Fields on `root.Config` via struct embedding                                                    | Package-level vars in `cmd/root.go`; initialized in `PersistentPreRunE`; passed to subcommands as getter functions |
| Post-parse init                       | Not supported                                      | Between `Parse()` and `Run()` in `cmd/cmd.go`                                                   | `PersistentPreRunE` on root (watch closest-ancestor trap)                                                          |
| Hook lifecycle                        | None                                               | Single `Exec` function                                                                          | `PersistentPreRunE → PreRunE → RunE → PostRunE → PersistentPostRunE`                                               |
| `-h` / `--help`                       | `flag.ErrHelp` returned unwrapped; `run()` exits 0 | `ff.ErrHelp` returned; `run()` exits 0                                                          | Cobra handles automatically                                                                                        |
| No subcommand given                   | Dispatcher prints command list                     | Dispatcher returns `ff.ErrNoExec`; `run()` treats as success                                    | Cobra prints help; returns nil                                                                                     |
| Group parent commands                 | Not supported                                      | `Exec: nil`; returns `ff.ErrNoExec`; `run()` treats as success                                  | `RunE: nil`; Cobra handles                                                                                         |
| Argument validation                   | Manual                                             | Manual                                                                                          | Built-in validators (`NoArgs`, `MinimumNArgs`, etc.)                                                               |
| Dispatch key                          | Exact `UsageLine` (case-sensitive)                 | `Name` field (case-insensitive)                                                                 | First word of `Use` (case-sensitive)                                                                               |
| Testability                           | Low — stdout not capturable                        | High — `cmd.Run` accepts `io.Reader`/`io.Writer`; stdin injectable                              | High — `Execute(ctx, args, stdout, stderr)`                                                                        |
| Signal handling                       | Manual                                             | `signal.NotifyContext` in `main`; `run()` returns `int`; `main()` calls `stop()` then `os.Exit` | Manual                                                                                                             |
| Controlled exit without error message | `os.Exit` in command (avoid)                       | `root.ExitError(n)` returned from `exec`                                                        | `os.Exit` in command (avoid)                                                                                       |
| Shell completions                     | None                                               | None                                                                                            | Built-in                                                                                                           |
| Man page / doc generation             | None                                               | None                                                                                            | Built-in (`doc.GenManTree`, `doc.GenMarkdownTree`)                                                                 |
| Code scaffolding                      | None                                               | `climax` tool                                                                                   | None                                                                                                               |

______________________________________________________________________

## 19. Checklist

### Architecture

- [ ] Root package: domain types, interfaces, Error type — no external imports
- [ ] Subpackages named after dependencies; each imports root, root imports none
- [ ] Subpackages never import each other
- [ ] Binaries under `cmd/<name>/main.go` when the project root is a domain package; for pure CLI tools with no domain package, `main.go` is at the project root and `cmd/` holds command sub-packages. No business logic in `main` either way.
- [ ] No global variables; dependencies injected explicitly (`var Version = "dev"` in `cmd/version/` is the one permitted exception)
- [ ] One major concept per file; ≤ 1000 SLOC per file

### Domain Types

- [ ] Service interfaces alongside their types in the same file
- [ ] Every interface method documents which error codes it returns
- [ ] Filter structs use pointer fields; Update structs use pointer fields
- [ ] `FindByID` returns `ENOTFOUND` when missing; never `(nil, nil)`
- [ ] `FindMany` returns `([]*T, int, error)` with total count
- [ ] `Create` mutates the input pointer (sets ID, timestamps)
- [ ] `Update` returns updated object even on error
- [ ] Caching/layering implemented as wrapper types satisfying domain interfaces

### Errors

- [ ] One `Error` type in root package: Code, Message, Op, Err
- [ ] Leaf errors carry Code + Message; wrapping errors carry Op + Err; never mixed
- [ ] Five base codes: ECONFLICT, EINTERNAL, EINVALID, ENOTFOUND, EUNAUTHORIZED
- [ ] `ErrorCode()` and `ErrorMessage()` helpers used; no direct type assertion
- [ ] External errors translated to domain codes at implementation boundary
- [ ] Every significant function wraps errors with `Op`

### HTTP

- [ ] `main` only calls `run`; `run` accepts OS primitives as parameters, Source: *(Shape A / HTTP services — Shape B dispatcher CLIs inject I/O at the dispatcher level; `run()` accepts only `ctx` and returns `int`)*
- [ ] `flag.NewFlagSet` inside `run`; global `flag` package unused
- [ ] `getenv` parameter used; no `os.Getenv` or `t.SetEnv` in tests
- [ ] All routes in one place; `/` has an explicit `http.NotFoundHandler()`
- [ ] `/healthz` endpoint present
- [ ] JSON encode/decode through central helpers in `encode.go`
- [ ] Handler return type is `http.Handler`; no durable state in closures
- [ ] Middleware with dependencies uses a constructor; no named type alias
- [ ] Graceful shutdown: `<-ctx.Done()` → `Shutdown` with timeout → `wg.Wait()`

### SQL

- [ ] `database/sql` used directly; for PostgreSQL, `pgx`/`sqlc` preferred over `database/sql`; no ORM; `pgx` not used with non-PostgreSQL databases
- [ ] Service methods thin: begin tx → helpers → commit; `defer tx.Rollback(ctx)` (pgx) or `defer tx.Rollback()` (database/sql)
- [ ] SQL helpers are unexported package-level functions accepting `pgx.Tx` / `districtsql.DBTX` (pgx) or `*Tx` (database/sql)
- [ ] Transactions not exposed to service callers
- [ ] `defer rows.Close()` immediately after successful query (pgx: `rows.Err()` checked via `pgx.CollectRows` or explicit loop; sqlc: handled automatically in generated code)
- [ ] `rows.Err()` returned after every manual row iteration loop
- [ ] Result slices initialized with `make([]*T, 0)`, not `var`
- [ ] WHERE clauses built with `[]string{"1 = 1"}` + `strings.Join`
- [ ] Sort order mapped from fixed named set; never interpolated from caller
- [ ] `COUNT(*) OVER()` used for total count in one query
- [ ] `attachXAssociations` helper used to load related data
- [ ] Cursor-based pagination; offset-based avoided
- [ ] No N+1 queries; JOINs or batch fetches used
- [ ] Authorization embedded in SQL WHERE clauses
- [ ] Parameterized queries throughout; no string interpolation
- [ ] Composite index design follows the prefix rule (highest-selectivity column first)

### Testing

- [ ] Table-driven tests used; every case named; `t.Run` wraps each case
- [ ] Tests are long and flat; helpers extracted only when truly universal
- [ ] `assert`, `ok`, `equals` helpers defined; no assertion library imported
- [ ] Test helpers: `t.Helper()`, no error return, cleanup as `func()`
- [ ] Test-specific helper types (e.g., `TestDB`) wrap real types with `t.Cleanup`
- [ ] Inline interfaces in tests implement only the methods needed
- [ ] Golden files used for complex output with `-update` flag; reviewed before commit
- [ ] Pure domain functions tested with real values; no mocks required
- [ ] Mocks hand-written in `mock` package with `Fn` + `Invoked` fields per method
- [ ] Interfaces defined at point of use, not in the implementing package; no wider than needed
- [ ] `internal/` used for test boundaries without public API exposure
- [ ] Real network connections used; `net.Conn` not mocked; `127.0.0.1:0` for port
- [ ] Production structs over-parameterized for test configurability
- [ ] No `time.Sleep`; async tests use `select` + `time.After` + `timeMultiplier`
- [ ] `t.Parallel()` only when tests are fully isolated (no shared global state)
- [ ] Acceptance/integration tests guarded by flag; run via `go test`
- [ ] Test files use `_test` package suffix
- [ ] Subprocess tests use `testHasX` guard or `helperProcess` mock pattern
- [ ] Public packages export test helpers in `testing.go` using `go-testing-interface`
- [ ] Contract comments (`// Requires:` / `// Ensures:`) on non-trivial functions
- [ ] No test-only seams added to production code to work around testability problems
- [ ] Tests not written to verify compiler type system behavior
- [ ] No `init()` used to set global state; no `sync.Once` global singletons tests must reset

### Distributed Systems

- [ ] Every mutating operation has a persisted idempotency key
- [ ] Timeouts set on all network calls
- [ ] Exponential backoff with jitter used for retries
- [ ] Retryable vs. non-retryable errors distinguished
- [ ] Circuit breakers around external service calls
- [ ] Bulkheads (separate limits per upstream) in place
- [ ] Monotonic clock used for elapsed time; wall clock not used for ordering
- [ ] No dual writes; CDC or event log used to propagate to derived systems
- [ ] No shared database across services (schema coupling avoided)
- [ ] Binary encoding for internal inter-service communication
- [ ] Fencing tokens used with distributed locks
- [ ] No language-native serialization across process boundaries
- [ ] Cache has TTL or explicit invalidation; not used as sole copy of data
- [ ] No database used as message queue
- [ ] Composite indexes designed with prefix rule in mind
- [ ] Linearizability only used where strictly required (locks, uniqueness, counters)
- [ ] Stream consumers are idempotent; processed message IDs persisted durably

### Observability

- [ ] RED metrics at p50/p95/p99 instrumented
- [ ] Structured JSON logs with correlation ID on every line
- [ ] Liveness and readiness endpoints distinct and present
- [ ] Distributed tracing in place for multi-service systems
- [ ] Data freshness tracked for batch/stream pipelines

### Integration Patterns

- [ ] Integration style chosen explicitly (Messaging / RPC / Shared DB / File Transfer) before interface design
- [ ] Business code isolated from messaging: Messaging Gateway (outbound), Service Activator (inbound)
- [ ] Coupling dimensions analyzed; specific remaining dimensions named; system not declared "loosely coupled" without specifying which dimensions
- [ ] All messages include Message ID and Correlation ID
- [ ] TTL set on time-sensitive messages
- [ ] Dead Letter Queue configured on every production consumer
- [ ] Idempotent Receiver implemented; processed IDs persisted durably (not in-memory)
- [ ] Queue message age monitored in addition to queue depth
- [ ] Flow control mechanism (TTL, tail drop, or backpressure) configured before queue capacity is needed
- [ ] EDA topology: Pub/Sub channels used where publisher-side independence is required; Recipient List pattern (EventBridge rules) acknowledged as a different trade-off

### Site Reliability Engineering

- [ ] SLI defined and measurable from user perspective; not from internal instrumentation
- [ ] SLO defined with rolling window; not set at 100%; tighter than SLA
- [ ] Error budget computed `(1 − SLO) × window` and tracked in real time
- [ ] Alerting structured in three tiers: pages (immediate action), tickets (days), logs (forensics only)
- [ ] Four golden signals instrumented: latency, traffic, errors, saturation
- [ ] Multi-window, multi-burn-rate alerting configured; not raw SLI violation alerting
- [ ] Toil tracked; any manual step repeated more than twice has an automation ticket
- [ ] On-call rotation has ≥8 engineers (single-site) or ≥6 (dual-site with follow-the-sun)
- [ ] Blameless postmortem written for every SLO-breaching incident; action items have owners and due dates
- [ ] Incident Commander role assigned at incident start; IC does not debug
- [ ] Production Readiness Review completed before SRE accepts on-call responsibility for a service
- [ ] Capacity planned 8–12 quarters ahead; N+2 redundancy modeled
- [ ] Changes deployed via staged rollout with automatic rollback on metric degradation

### CLI Commands

- [ ] `go.mod` checked to select pattern: Cobra → Pattern C; ff/v4 → Pattern B; neither → Pattern A. Check Cobra first — it takes precedence if both are present.
- [ ] No CLI framework introduced that is not already in `go.mod`
- [ ] All commands return `error`; no `os.Exit` inside a command
- [ ] Error strings: lowercase, no trailing punctuation, format `<command>: <reason>`
- [ ] **Pattern A:** `flag.NewFlagSet("name", flag.ContinueOnError)` — never `flag.ExitOnError`
- [ ] **Pattern A:** `flag.ErrHelp` returned **unwrapped**; `main` catches with `errors.Is(err, flag.ErrHelp)` and exits 0
- [ ] **Pattern A:** No redundant `fs.Usage()` call — `flag.ContinueOnError` auto-prints usage on `-h`
- [ ] **Pattern A:** `<Name>Command()` factory is the only exported symbol; logic in unexported `<name>Cmd`
- [ ] **Pattern A:** All commands registered in `cmd/cmd.go` commands slice; no `init()`
- [ ] **Pattern B:** `SetParent(parent.Flags)` called on every subcommand's flag set (including when parent has no flags — `SetParent(nil)` is safe)
- [ ] **Pattern B:** I/O via `cfg.Stdout`/`cfg.Stderr` only; never `os.Stdout`/`os.Stderr`
- [ ] **Pattern B:** Dispatcher returns `ff.ErrNoExec` as-is; `run()` in `main.go` treats it as success — dispatcher does not print help for this case
- [ ] **Pattern B:** Flag values bound in `New()`, not in `exec`; `exec` reads already-parsed values
- [ ] **Pattern B:** Every configurable knob is a registered flag on `cfg.Flags`; no `os.Getenv` or hard-coded values outside the flag set
- [ ] **Pattern B:** `root.ExitError(n)` returned from `exec` for controlled non-zero exits; never `os.Exit` inside command code
- [ ] **Pattern B:** `run()` receives `stdin`, `stdout`, `stderr` from `main()` and forwards them to `cmd.Run`; inject `strings.NewReader("")` and `&bytes.Buffer{}` (or `io.Discard`) into `run()` in tests, not into `cmd.Run` directly.
- [ ] **Pattern B:** `main.go` uses `signal.NotifyContext`; `run()` returns `int`; `main()` calls `stop()` then `os.Exit(code)` — `os.Exit` inside `run()` would bypass `stop()`, leaving the signal-handler goroutine running
- [ ] **Pattern B:** Climax marker comments preserved in `cmd/cmd.go`: `// climax:name`, `// climax:root-pkg`, `// climax:imports`, `// register new commands here`
- [ ] **Pattern C:** No package-level `var rootCmd`; flag-destination variables declared inside `NewCommand`
- [ ] **Pattern C:** `RunE` used (not `Run`); `Args` validator set explicitly on every command
- [ ] **Pattern C:** Root command has `SilenceErrors: true` and `SilenceUsage: true`
- [ ] **Pattern C:** `cmd.Usage()` called only for genuine usage errors (wrong arg count, invalid flag) — never for runtime errors
- [ ] **Pattern C:** Getter functions used for cross-package shared state (e.g. `func() *api.Client { return sharedClient }`) — never pass the pointer directly at construction time
- [ ] **Pattern C:** `cobra.EnableTraverseRunHooks = true` set before `ExecuteContext` if any subcommand defines its own `PersistentPreRunE`
- [ ] **Pattern C:** Integration tests drive commands through `cmd.Execute`; unit tests of individual subcommands inject dependencies explicitly (root's `PersistentPreRunE` does not run without a root parent)
