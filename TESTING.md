# Testing

Workshop follows a **plan → spec → tests → build** discipline. This document is
the practical how-to; the conceptual workflow and per-area specs live in
[`docs/specs/`](docs/specs/README.md).

## Workflow

1. **Plan** — file a ticket, sketch what needs to change.
2. **Spec** — update the relevant file under `docs/specs/` with the target
   behavior, data model changes, invariants, and edge cases. The spec is
   the contract the tests and implementation both answer to.
3. **Tests** — write failing unit + integration tests against the spec
   before touching implementation. Bug fixes MUST include a regression
   test that fails before the fix and passes after.
4. **Build** — implement until tests pass. If reality disagrees with the
   spec, update the spec in the same commit — never silently diverge.

## Layers

- **Unit** — a single function or component in isolation. No DB, no
  network, no filesystem (except `t.TempDir()`). Fast. Lives next to the
  source file as `foo_test.go` or `Foo.test.tsx`.
- **Integration** — multiple units wired together with real collaborators:
  ephemeral SQLite, in-process HTTP server, mock tmux bridge. Backend
  integration tests use the `integration` build tag so they can be
  excluded from the fast path. Frontend integration tests live under
  `frontend/src/test/integration/`.
- **End-to-end** — full stack, real server, real browser. Used sparingly.
  Not yet set up; most behavior coverage will come from integration tests.

## Backend (Go)

### Writing tests

- Put `_test.go` files next to source.
- Prefer table-driven tests with `t.Run` subtests.
- Use [`testify`](https://github.com/stretchr/testify) (`require`/`assert`).
- Use helpers from `internal/testhelpers`:
  - `testhelpers.TempDB(t)` — ephemeral SQLite `*db.DB` with automatic
    cleanup. Every test that touches the database should use this.
  - `testhelpers.TempDataDir(t)` — raw tempdir if you don't need a DB.
- Integration tests: tag the file with `//go:build integration` so they
  only run under `make test-integration`.

### Running

```bash
make test             # default: fast unit tests
make test-unit        # explicit unit tests
make test-integration # only integration-tagged tests
make test-race        # -race over the full tree
make test-cover       # coverage report → coverage/backend.html
```

## Frontend (React + TypeScript)

### Writing tests

- Put `Foo.test.tsx` next to `Foo.tsx`.
- Use [Vitest](https://vitest.dev/) + [React Testing Library](https://testing-library.com/react).
- Import jest-dom matchers implicitly (registered in `src/test/setup.ts`).
- Prefer `userEvent` over fireEvent for interactions.
- Mock the API client with `vi.mock('../api/client', …)` or a shared
  stub.
- `localStorage` is cleared between tests automatically.

### Running

```bash
cd frontend
npm test              # run once
npm run test:watch    # watch mode
npm run test:ui       # Vitest UI
npm run test:cover    # coverage report → frontend/coverage/
```

Or from the repo root: `make test-frontend`.

## Pre-push hook

Workshop ships a checked-in git hook that runs fast tests before every push.
Install it once after cloning:

```bash
make install-hooks
```

This sets `git config core.hooksPath .githooks` so `.githooks/pre-push`
takes effect. It runs `make test-unit` followed by `make test-frontend`
and aborts the push on any failure.

**Emergency bypass**: `git push --no-verify`. Use sparingly — pushing a
broken tree is how we get into "main is always green" regressions.

## PR / commit checklist

Before pushing any change that touches behavior:

- [ ] Spec updated if the behavior changed
- [ ] New tests cover the change (or a note explaining why not)
- [ ] Bug fixes include a regression test that fails without the fix
- [ ] `make test-unit` passes
- [ ] `make test-frontend` passes
- [ ] `make test-race` passes (optional locally; run for concurrency work)

## Coverage expectations

We do **not** enforce a coverage percentage. Instead: every merged change
either adds meaningful tests OR the commit message explains why it
doesn't need them (pure refactor, config tweak, docs, etc.). The spec
files in `docs/specs/` carry a **Test matrix** section that lists what we
want pinned — those are the things the tests should cover.

## Adding a new feature area

1. Create `docs/specs/<area>.md` following the structure in
   [`docs/specs/README.md`](docs/specs/README.md).
2. Link it from `docs/specs/README.md`'s table of contents and from
   `FEATURES.md`.
3. Fill in the test matrix with the scenarios that will prove the spec.
4. Write the tests.
5. Build the feature.

## Current test surface

The test foundation is in place (`internal/testhelpers/`,
`frontend/src/test/setup.ts`), but most feature areas still have only
smoke tests. The per-area spec + test tickets (#481–#490) will fill this
in area by area.
