# Issue 1417 residual investigation

## Scope and replay

- Worktree: `C:\Users\16382\Documents\ChatGPT\magpie\worktrees\issue-1417-residual-20261026`
- Base: `3b717d5e0d62a73af365a84ace0eed295809918a` (latest upstream `main` supplied for this task)
- Source patch: `20c79691dee427fe25d914c3e0dd8cd6bd279de8`
- The worktree is detached.
- `git cherry-pick 20c79691dee427fe25d914c3e0dd8cd6bd279de8` completed with no conflicts and created `5b158e248aeb60ddf8d3a6619334fd95b4fdf732`.
- The replayed commit kept the same subject and changed:
  - `internal/provider/commandcode_catalog.go`
  - `internal/provider/commandcode_catalog_test.go`
  - `internal/provider/models.go`
- The retired `commandcode-plan` built-in was not restored.
- No issue comment, push, pull request, issue closure, real credentials, Magpie port 3425 instance, or PID 17684 was used.
- Issue #1417's author is not yetone; this evidence does not attribute the report to yetone.

## Is the residual user-visible?

No. The saved endpoint metadata can contain plan-excluded models, but those
models are not available to model selection or routing through that metadata.
No production cleanup is warranted.

Evidence:

1. `fetchOne` merges each endpoint's list into `out`, computes
   `parts = sidesOf(sides)` from the endpoint lists, and applies
   `cmdKeepPlan` only to the final `fetched.models` value
   (`internal/provider/models.go`, around `fetchOne`'s return). Thus
   `models/<provider>.json` can have a filtered `Models` list and unfiltered
   `Sides` parts. This is intentional endpoint provenance for keeping a
   failed endpoint's own models.
2. `Provider.available`, `live`, and `LiveByID` read `f.Models`, not `f.Sides`
   (`internal/provider/models.go`, `internal/catalog/live.go`).
   `Provider.Available` therefore cannot surface a model that only survives
   in `Sides`. `Provider.Exposed` selects from `Available`.
3. `Provider.ListedAPIs` uses `liveByID`, so a protocol route cannot be
   inferred from `Sides` for an otherwise filtered model.
4. `/api/providers` builds each provider row from `Available`, `Exposed`, and
   `ListedAPIs`; `providersJSON` has no `sides` field
   (`internal/gui/providers.go`).
5. `catalog.LiveSplit` has two production callers:
   - `keepDeadSides`: a failed endpoint's saved part is merged back into
     `out`, after which `fetchOne` applies `cmdKeepPlan` again.
   - `fetchPerKey`: each key's `fetchOne` already returns filtered models;
     the merged `out` is filtered and the sides it saves are reconstructed
     from that merged filtered list.
6. The explicit-list path (`ModelsURL`) also filters with `cmdKeepPlan` and
   saves no sides.

Boundary: an explicitly saved model pick or an explicit `provider/model`
request remains permissive under the existing provider design. That is not a
leak through `Sides`, and changing it would be a separate behavioral change.

## Changes

Production code after the cherry-pick is unchanged. The residual work adds
regression evidence only:

- `internal/provider/commandcode_catalog_test.go`
  - `TestCommandCodePlanFilterStaysOnUserSurfacesWhenSidesKeepMore`
  - Saves a filtered model list with unfiltered sides, checks `Available`,
    `Exposed`, and `ListedAPIs`, repeats after an endpoint failure forces a
    saved side to be kept, and checks fail-open when a new key's plan read
    fails.
- `internal/gui/provider_commandcode_sides_test.go`
  - `TestCommandCodeSavedSidesStayOutOfProvidersAPI`
  - Saves allowed `models` with unfiltered `sides`, calls the real
    `GET /api/providers` handler, and checks excluded models are absent.

## Red, green, mutation

The first targeted test proves the residual boundary is exercised: it reads
`catalog.LiveSplit`, fails if the fixture does not retain excluded models in
the saved sides, and logs:

```text
saved sides still name plan-excluded models; user surfaces must not
```

Mutation run: `cmdKeepPlan` was temporarily changed to return its input
unchanged. The test failed with:

```text
=== RUN   TestCommandCodePlanFilterStaysOnUserSurfacesWhenSidesKeepMore
    commandcode_catalog_test.go:217: first fetch: [deepseek/deepseek-v4.1-flash claude-sonnet-5-5 claude-sonnet-5 gpt-6.1-sol], want [deepseek/deepseek-v4.1-flash claude-sonnet-5-5]
--- FAIL: TestCommandCodePlanFilterStaysOnUserSurfacesWhenSidesKeepMore (0.05s)
FAIL
```

The mutation was reverted (`git diff --exit-code --
internal/provider/commandcode_catalog.go` was clean), and the same test
passed.

Because the investigation found no production defect requiring a fix, there
is no production red-to-green implementation change. The mutation establishes
that the residual guard fails when plan filtering is removed.

## Verification

All commands used PowerShell. Go version: `go1.27.0 windows/amd64`.

Passed:

```text
gofmt -l <changed Go files>
  (no output)
git diff --check
  (no output)

go test -tags nogui ./internal/provider -run '^(TestCommandCode.*|TestFetchUnions.*|TestFetchKeeps.*|TestFetchAsksEachAuthenticationAtOneBase)$' -count=1 -v
  PASS, all selected provider tests

go test -tags nogui ./internal/provider -run '^TestCommandCodePlanFilterStaysOnUserSurfacesWhenSidesKeepMore$' -count=1 -v
  PASS after mutation was reverted

go test -tags nogui ./internal/catalog -run '^TestLiveSplitKeepsEachEndpoint$' -count=1 -v
  PASS

go test -tags nogui ./internal/gui -run '^TestCommandCodeSavedSidesStayOutOfProvidersAPI$' -count=1 -v
  PASS

go test -tags nogui ./internal/gateway -run '^(TestCommandCodeGoKeySaysWhereItWorks|TestCommandCodePluginReasoning|TestCommandCodeZDRRefusalRestsNothing|TestZDRRefusalIsCommandCodesCodeOnly|TestZDRRefusalNeverRests|TestCommandCodeSearchesForSomeModels|TestCommandCodeSearchesForOtherModels|TestGroupFailsOverCommandCodesBare400|TestSystemMessageRefusedGoesOnTop|TestSystemMessageKeptWhereTaken)$' -count=1 -v
  PASS

go vet ./...
go build ./...
go vet -tags nogui ./...
go build -tags nogui ./...
GOOS=linux CGO_ENABLED=0 go vet -tags nogui ./...
GOOS=linux CGO_ENABLED=0 go build -tags nogui ./...
  all passed
```

The Bun-dependent `TestCommandCodeZDRRefusalRestsNothing` subtests were
reported as `SKIP: no bun on PATH`; the test itself passed while those
subtests skipped.

Package-level failures observed on this Windows host, unrelated to this
change:

```text
go test -tags nogui ./internal/provider -count=1
  FAIL after 174.916s:
  TestCodexListTagKeptWhileNothingChanged (allocation threshold)
  TestCodexSignedInInKeychain
  TestCodexKeychainRefreshWritesItem
  TestCodexKeychainSwitch
  TestCodexKeychainAskedSeldom
  TestCodexKeychainPromptWaitedFor
  TestCodexKeychainRetry
  TestCodexKeychainSignOut
  TestCodexSignInKnown
  TestSavedButSignedOut

A full ./internal/gateway run was attempted earlier in this worktree and
timed out after 10 minutes in the unrelated
TestClaudeMemRunsFromTheCgroup, stuck in claude_room.go readLimit.
```

## Not tried

- No real Command Code key or vendor API was used.
- No real account plan, credits, or catalog response was read.
- Bun-dependent gateway plugin tests were not run because Bun is not on PATH.
- Race tests were not run. This host has no gcc/CGO toolchain, and the user
  explicitly requires race to be recorded as `Not tried`.
- The full `internal/gateway` suite was not repeated after its unrelated
  10-minute timeout; the relevant Command Code and system-message tests were
  run directly and passed.

## Final state

- The replay commit is `5b158e248aeb60ddf8d3a6619334fd95b4fdf732`.
- This evidence is committed with the two test files. The final HEAD is the
  commit containing this file (`git rev-parse HEAD`).
- No production source change was needed for the residual `sides` question.
