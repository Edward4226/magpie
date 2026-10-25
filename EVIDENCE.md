# Evidence: Ark plan windows in key routing

## Base

- Worktree: `C:\Users\16382\Documents\ChatGPT\magpie\worktrees\issue-ark-plan-routing-20261026`
- Base: `3b717d5e0d62a73af365a84ace0eed295809918a` (`v0.1.1554`)
- Base is detached. No issue comment, push, PR, or close was attempted.
- The live Magpie process PID 3425 was not used. Real Ark credentials and the
  live Magpie instance were not used.

## What was true at the base

Ark's Coding Plan and Agent Plan cards came from `volcPlanQuotas` in
`internal/provider/volcengine_usage.go`. That function read the account
control plane with `GetCodingPlanUsage` or `GetAFPUsage`, using
`AccessKeyID`/`SecretAccessKey`, and returned Usage cards.

The cards did not enter routing:

- `volcPlanQuotas` never called `noteKeyAllowance`.
- `readsKeyWindows` accepted sub2api keys and `planQuotaSourceOf` plan keys,
  not Ark.
- `keyWindows` could therefore not read the Ark action for routing.
- `candidate.allowance` in `internal/gateway/routing.go` saw Ark as unknown.

This matches #1427's explicit statement that routing was left for a separate
change, and #3251's second layer: after the written `+0800 CST` reset was
fixed, an Ark quota refusal with no other clue still fell back to
`quotaRest` (15 minutes) because the key had no routing allowance.

## AccessKey to inference key: evidence and boundary

What the code and issues prove:

- The AccessKey identifies the Volcengine account/control plane, not an Ark
  inference key. `Provider.AccessKeyID`/`SecretAccessKey` are deliberately
  separate from `Provider.Key`.
- Ark exposes no per-inference-key quota API. The plan usage endpoint reports
  an account-wide 5-hour, weekly, and monthly window.
- The plan action and region come from the configured endpoint:
  `/api/coding/` means `GetCodingPlanUsage`, `/api/plan/` means
  `GetAFPUsage`, and the region comes from `ark.<region>.volces.com`.
- #2849 says plan and region are provider-level settings. A provider may hold
  several inference keys whose plans differ, and no existing field records
  which plan/account each key belongs to.

Therefore fanning an AccessKey card out to every key would be unsafe. A
provider can hold Coding Plan, Agent Plan, and pay-as-you-go keys together.

The patch's narrow operational rule is:

- Bridge only when the provider has exactly one inference key **in use** and
  has both AccessKey fields.
- If `p.Key` appears in `p.Keys`, treat it as a selected member of a
  multi-key provider (the `WithKey` shape) and do not bridge.
- If another non-empty key is on, do not bridge.
- Off extra keys do not count as another key in use.
- Never share the allowance by AccessKey across providers or to another key.

This is not cryptographic proof that the sole inference key and AccessKey
belong to the same Volcengine account. It is an explicit operator-level
assumption for a one-key provider. Making it enforceable needs a schema
decision, which is left to the owner (see "Owner decisions").

## Change

Provider bridge:

- `internal/provider/volcengine_usage.go`
  - Added `volcKeyPlanOf`, the conservative one-key predicate above.
  - After a successful `volcPlanQuota` read, call `noteKeyAllowance` before
    `keepReading`, so routing sees the fresh card and a stale previous card
    cannot overwrite it.
  - Persist an Ark card under action, region, and AccessKey fingerprint.
- `internal/provider/key_allowance.go`
  - `readsKeyWindows` accepts the safe Ark one-key case.
  - `keyWindows` reads `GetCodingPlanUsage`/`GetAFPUsage` through
    `volcPlanQuota`.
  - Allowance identity now includes provider, inference-key fingerprint,
    action, region, and AccessKey fingerprint.
  - `keyCardTag` seeds a restarted process from the same persisted Ark card.

Documentation and UI:

- `docs/subsystems/providers-accounts.md`
- `docs/subsystems/gateway-routing.md`
- `internal/gui/assets/app.js`
- `internal/gui/assets/i18n.js`
- `internal/gui/tests/README.md`
- `internal/gui/tests/volc-access-key.test.cjs`

New tests:

- `internal/provider/volcengine_key_allowance_test.go`

The written-reset behavior for Ark `...+0800 CST` was not changed.

## Failure propagation

- API, auth, HTTP, parse failure, or a reply with no recognized windows sets
  the card's `Error`. `keyWindows` returns an error. `readKeyAllowance` keeps
  the previous `Allowance`; it does not reinterpret the failure as "no
  windows".
- A successful Usage read is noted immediately. If a later read fails,
  `keepReading` may put the older card back on the Usage page, but the failed
  read never notes that older card as a new reading.
- After restart, the last good persisted card seeds routing. A failed
  background read leaves that seed in place.
- Missing credentials, a multi-key provider, or a selected non-first key
  makes Ark unknown to routing instead of borrowing another key's windows.
- An AccessKey shared by two provider records is not fanned out. Only the
  provider whose Usage card was actually read is noted.

## Sibling sites checked

- `volcPlanQuotas`: now calls `noteKeyAllowance` only on successful reads.
- Plan quota jobs in `internal/provider/planquota.go`: already note a
  successful card at once; the Ark path follows that contract.
- `noteKeyAllowance`: remains the single insertion point for cards read by
  the Usage path.
- `readsKeyWindows` / `keyWindows`: both now admit only the safe one-key Ark
  case.
- `KeyAllowance`, `keyAllowanceID`, `keyCardTag`: identity and disk seeding
  include the control-plane source.
- Gateway routing consumer: `candidate.allowance`, `candidate.full`,
  `weighRouted`, `restAfterMarked`, and `renewed` use the same
  provider-neutral key allowance contract; no gateway routing policy was
  changed.
- #2849 per-key plan/region: treated as the hard boundary. A provider with a
  selected or additional active key stays unknown.
- `tencentPlanQuotas`: checked as the other control-plane card of this
  shape, but intentionally not bridged. It has the same account-to-key
  identity problem and is outside this Ark issue.

## Red, green, and mutation

Before the bridge, the new tests were red:

```text
TestVolcKeyAllowanceFromItsPlanCard:
  after the card's read: false []
TestVolcKeyAllowanceReadBehindRouting:
  the key's windows were never read
TestVolcKeyAllowanceNotFannedOutByAccessKey:
  first allowance not noted
```

Mutation: remove the one-key guard from `volcKeyPlanOf`, then run:

```powershell
go test -tags nogui ./internal/provider -run '^TestVolcKeyAllowanceLeavesMultiKeyProviderUnknown$' -count=1
```

It failed as intended:

```text
--- FAIL: TestVolcKeyAllowanceLeavesMultiKeyProviderUnknown
    ark-a's key reads windows
    ark-b's key reads windows
    2 key allowances noted for a provider with several keys
FAIL
```

The guard was restored and the same test passed.

Green focused checks:

```text
go test -tags nogui ./internal/provider -run 'TestVolc|TestKeyAllowance|TestPlanKeyAllowance|TestPayAsYouGoKeyAskedSeldom' -count=1
ok  github.com/yetone/magpie/internal/provider

go test -tags nogui ./internal/gateway -run 'TestResetsWritten|TestMonthlyWrittenResetRestsTheMonth|TestLoneAllowance|TestKeyAllowance|TestKeyWindow' -count=1
ok  github.com/yetone/magpie/internal/gateway

go test -tags nogui ./internal/gui -run 'TestProviderSaveKeepsVolcengineSecret|TestProviderList' -count=1
ok  github.com/yetone/magpie/internal/gui
```

Formatting, static checks, and builds:

```text
gofmt -l internal/provider/volcengine_usage.go internal/provider/key_allowance.go internal/provider/volcengine_key_allowance_test.go
(no output)
git diff --check
(no output)
GOOS=windows GOARCH=amd64 go vet -tags nogui ./internal/provider ./internal/gateway
(pass)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet -tags nogui ./internal/provider ./internal/gateway
(pass)
GOOS=windows GOARCH=amd64 go build -tags nogui ./...
(pass)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags nogui ./...
(pass)
node --check internal/gui/assets/app.js
node --check internal/gui/assets/i18n.js
node --check internal/gui/tests/volc-access-key.test.cjs
(pass)
```

The full `internal/provider` package is not green on this Windows host. The
new Ark tests pass. The failing tests are:

```text
TestCodexListTagKeptWhileNothingChanged
TestCodexSignedInInKeychain
TestCodexKeychainRefreshWritesItem
TestCodexKeychainSwitch
TestCodexKeychainAskedSeldom
TestCodexKeychainPromptWaitedFor
TestCodexKeychainRetry
TestCodexKeychainSignOut
TestCodexSignInKnown
TestSavedButSignedOut
```

The same exact tests fail at base `3b717d5e` with the worktree changes
temporarily stashed. The output is identical in kind: keyring support is
unavailable and the allocation guard fails. They do not touch the Volcengine
or allowance paths.

## Not tried

- Real Volcengine Ark API calls: no real Ark credentials were used.
- A real Coding Plan, Agent Plan, or multi-key Ark account.
- Whether a real Ark inference key can be proven to belong to the configured
  AccessKey's account.
- `go test -race`: CGO/gcc is not available on this Windows host.
- Playwright: `playwright` is not installed. The edited browser assets and
  test file pass `node --check`, but the Chromium/WebKit GUI suite was not
  run.
- The full gateway package suite. The directed gateway routing tests pass.

## Owner decisions

1. Is "one inference key in use" an acceptable implicit statement that its
   AccessKey reads the same account? If not, routing must remain disabled
   until there is an explicit key-to-account/plan/region field.
2. How should a multi-key Ark provider explicitly map each inference key to
   an AccessKey, plan, and region? This belongs with #2849 and should not be
   guessed from key order.
3. Are the expanded allowance and card identities (action, region, AccessKey
   fingerprint) acceptable stable cache/rest identities? They prevent stale
   or cross-account reuse but invalidate an older Ark card seed once.
