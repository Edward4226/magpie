# Issue #3491 path-fix evidence

## Baseline

- Original base commit: `3b717d5e0d62a73af365a84ace0eed295809918a`
- Final rebased base: `7499ebb5ef08704d663ec70c6be45884197cc03f`
- Worktree: `C:\Users\16382\Documents\ChatGPT\magpie\worktrees\issue-3491-path-20261026`
- Detached HEAD at the base commit.
- All commands were run from PowerShell with a temporary `HOME`; `GOPATH`,
  `GOMODCACHE`, and `GOCACHE` were pinned from the existing `go env` values.

## Root cause

`OpenCodeFreeURL` always rewrote a Zen URL's `/zen` prefix to `/inference`.
The current catalog advertises the OpenCode Zen OpenAI base as
`https://opencode.ai/inference/openai/v1`, so Chat requests were still sent
to the old `/inference/v1` path. The same helper serves both live gateway
requests and the provider Test button.

The fix passes the request protocol shape into `OpenCodeFreeURL`. Only
`Chat` uses the `/inference/openai` prefix. Responses, Messages, Gemini,
Go, relay/custom hosts, and URLs already at the correct path retain their
existing behavior. No affinity or User-Agent behavior was changed.

## Changed files

- `internal/provider/opencode_client.go`: protocol-aware free URL rewrite.
- `internal/provider/test.go`: pass the Test button's protocol shape.
- `internal/gateway/gateway.go`: pass the live request's protocol shape.
- `internal/provider/opencode_zen_test.go`: path matrix, Test-button
  assertion, and fixture expectations.
- `internal/gateway/zenfree_test.go`: live gateway path assertion.

## Sibling sites checked

- Live gateway conversion: `internal/gateway/gateway.go`, in `forwardOnce`.
- Test-button conversion: `internal/provider/test.go`, in `tiny`.
- Provider URL matrix and public-model fixture:
  `internal/provider/opencode_zen_test.go`, including `zenFake`.
- Gateway routing fixture:
  `internal/gateway/zenfree_test.go`, `TestZenFreeAtInferenceURL`.
- No other `OpenCodeFreeURL` callers were present.

## Red before the fix

The new assertions were first run with a zero-behavior bridge: the helper
accepted the protocol parameter but still used the old `/inference`
rewrite. This proves the new tests are red against the actual old behavior.

```text
PS> go test -tags nogui ./internal/provider -run '^TestOpenCodeFreeURL$' -count=1
--- FAIL: TestOpenCodeFreeURL (0.00s)
    opencode_zen_test.go:169: chat base: chat: https://opencode.ai/inference/v1, want https://opencode.ai/inference/openai/v1
    opencode_zen_test.go:169: chat base slash: chat: https://opencode.ai/inference/v1/, want https://opencode.ai/inference/openai/v1/
    opencode_zen_test.go:169: chat endpoint: chat: https://opencode.ai/inference/v1/chat/completions, want https://opencode.ai/inference/openai/v1/chat/completions
    opencode_zen_test.go:169: chat root: chat: https://opencode.ai/inference, want https://opencode.ai/inference/openai
    opencode_zen_test.go:169: api subdomain: chat: https://api.opencode.ai/inference/v1, want https://api.opencode.ai/inference/openai/v1
    opencode_zen_test.go:174: chat probe https://opencode.ai/inference/v1/chat/completions
FAIL
FAIL	github.com/yetone/magpie/internal/provider	1.881s

PS> go test -tags nogui ./internal/provider -run '^TestZenPublicTestAsksFreeModels$' -count=1
--- FAIL: TestZenPublicTestAsksFreeModels (0.21s)
    opencode_zen_test.go:295: chat: {Account: Protocol:chat OK:false Status:401 Millis:0 Model:big-pickle Error:401 Unauthorized} (asked [/inference/v1/chat/completions big-pickle /inference/v1/responses muse-spark-1.2-contributor-free])
FAIL
FAIL	github.com/yetone/magpie/internal/provider	2.048s

PS> go test -tags nogui ./internal/gateway -run '^TestZenFreeAtInferenceURL$' -count=1
--- FAIL: TestZenFreeAtInferenceURL (0.17s)
    zenfree_test.go:286: paths [opencode.ai/inference/v1/chat/completions opencode.ai/zen/v1/chat/completions]
FAIL
FAIL	github.com/yetone/magpie/internal/gateway	2.622s
```

## Green after the fix

```text
PS> go test -tags nogui ./internal/provider -run '^TestOpenCodeFreeURL$' -count=1
ok  	github.com/yetone/magpie/internal/provider	1.875s

PS> go test -tags nogui ./internal/provider -run '^TestZenPublicTestAsksFreeModels$' -count=1
ok  	github.com/yetone/magpie/internal/provider	2.228s

PS> go test -tags nogui ./internal/gateway -run '^TestZenFreeAtInferenceURL$' -count=1
ok  	github.com/yetone/magpie/internal/gateway	2.645s

PS> go test -tags nogui ./internal/provider -run '^(TestOpenCode|TestZen)' -count=1
ok  	github.com/yetone/magpie/internal/provider	2.957s

PS> go test -tags nogui ./internal/gateway -run '^(TestOpenCode|TestZenFree)' -count=1
ok  	github.com/yetone/magpie/internal/gateway	4.894s
```

After restoring the mutation, the exact targeted assertions were run again:

```text
PS> go test -tags nogui ./internal/provider -run '^(TestOpenCodeFreeURL|TestZenPublicTestAsksFreeModels)$' -count=1
ok  	github.com/yetone/magpie/internal/provider	2.070s

PS> go test -tags nogui ./internal/gateway -run '^TestZenFreeAtInferenceURL$' -count=1
ok  	github.com/yetone/magpie/internal/gateway	2.668s
```

## Mutation

Mutation applied after the green run: the Chat branch was changed from
`if api == Chat` to `if api == Chat && false`, leaving every other behavior
unchanged. The new assertions failed:

```text
PS> go test -tags nogui ./internal/provider -run '^TestOpenCodeFreeURL$' -count=1
--- FAIL: TestOpenCodeFreeURL (0.00s)
    opencode_zen_test.go:169: chat base: chat: https://opencode.ai/inference/v1, want https://opencode.ai/inference/openai/v1
    opencode_zen_test.go:169: chat base slash: chat: https://opencode.ai/inference/v1/, want https://opencode.ai/inference/openai/v1/
    opencode_zen_test.go:169: chat endpoint: chat: https://opencode.ai/inference/v1/chat/completions, want https://opencode.ai/inference/openai/v1/chat/completions
    opencode_zen_test.go:169: chat root: chat: https://opencode.ai/inference, want https://opencode.ai/inference/openai
    opencode_zen_test.go:169: api subdomain: chat: https://api.opencode.ai/inference/v1, want https://api.opencode.ai/inference/openai/v1
    opencode_zen_test.go:174: chat probe https://opencode.ai/inference/v1/chat/completions
FAIL
FAIL	github.com/yetone/magpie/internal/provider	1.958s

PS> go test -tags nogui ./internal/provider -run '^TestZenPublicTestAsksFreeModels$' -count=1
--- FAIL: TestZenPublicTestAsksFreeModels (0.22s)
    opencode_zen_test.go:295: chat: {Account: Protocol:chat OK:false Status:401 Millis:0 Model:big-pickle Error:401 Unauthorized} (asked [/inference/v1/chat/completions big-pickle /inference/v1/responses muse-spark-1.2-contributor-free])
FAIL
FAIL	github.com/yetone/magpie/internal/provider	2.325s

PS> go test -tags nogui ./internal/gateway -run '^TestZenFreeAtInferenceURL$' -count=1
--- FAIL: TestZenFreeAtInferenceURL (0.18s)
    zenfree_test.go:286: paths [opencode.ai/inference/v1/chat/completions opencode.ai/zen/v1/chat/completions]
FAIL
FAIL	github.com/yetone/magpie/internal/gateway	2.638s
```

The mutation was then removed, and the targeted green tests above were
rerun successfully.

## Formatting and static checks

```text
PS> gofmt -w internal/provider/opencode_client.go internal/provider/test.go internal/gateway/gateway.go internal/provider/opencode_zen_test.go internal/gateway/zenfree_test.go
gofmt exit=0

PS> git diff --check
git diff --check exit=0

PS> go vet -tags nogui ./...
windows vet exit=0

PS> GOOS=linux CGO_ENABLED=0 go vet -tags nogui ./...
linux vet exit=0
```

## Builds

Build outputs were written under `%TEMP%\magpie-issue3491-path-build`.

```text
PS> go build -tags nogui -o <temp>/magpie-windows-amd64.exe .
windows build exit=0

PS> go build -o <temp>/magpie-windows-amd64-gui.exe .
windows default build exit=0

PS> GOOS=linux CGO_ENABLED=0 go build -tags nogui -o <temp>/magpie-linux-amd64 .
linux build exit=0
```

## Full package runs and limits

`go test -tags nogui ./internal/provider -count=1` was attempted. It
reported unrelated platform/environment failures, including Codex keychain
tests and `TestCodexListTagKeptWhileNothingChanged`; the focused
OpenCode/Zen provider tests above passed independently.

`go test -tags nogui ./internal/gateway -count=1` was attempted. It timed
out after 10 minutes in unrelated `TestClaudeMemRunsFromTheCgroup`; the
focused OpenCode/ZenFree gateway tests above passed independently.

## Not tried

- `go test -race`: not tried because no gcc is installed, as required by
  the task.
- Full repository `go test -tags nogui ./...`: not tried because the full
  provider and gateway package runs already hit unrelated environment
  failures/timeouts.
- Linux test execution: not tried; Linux was cross-vetted and cross-built
  only.
- Live free-model request after the path change: not tried. No user
  credentials were read, and no claim is made about
  `FreeUsageLimitError`.

## Out of scope / risk

- This change only aligns the Chat/OpenAI-shaped free endpoint path.
- It does not restore affinity or User-Agent spoofing and does not claim to
  solve HTTP 429 or `FreeUsageLimitError`.
- The separate local long-circuit-breaker issue is not changed in this
  worktree.
