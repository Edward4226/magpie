package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// cmdPlainListReply is what api.commandcode.ai/provider/v1/models answers:
// the vendor's models, with no word of which plan has which.
const cmdPlainListReply = `{"object":"list","data":[
 {"id":"deepseek/deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash"},
 {"id":"claude-sonnet-5-5","name":"Claude Sonnet 5.5"},
 {"id":"claude-sonnet-5","name":"Claude Sonnet 5"},
 {"id":"gpt-6.1-sol","name":"GPT-6.1 Sol"}]}`

// cmdClientListReply is ?view=client's, which names each model's plan.
const cmdClientListReply = `{"catalogVersion":"test","refreshAfterSeconds":900,"roles":{},"data":[
 {"id":"deepseek/deepseek-v4.1-flash","category":"opensource","minimumPlan":"individual-go"},
 {"id":"claude-sonnet-5-5","category":"opensource","minimumPlan":"individual-goat"},
 {"id":"claude-sonnet-5","category":"premium","minimumPlan":"individual-pro"},
 {"id":"gpt-6.1-sol","category":"premium","minimumPlan":"individual-max"}]}`

// cmdGoatPlan, cmdNoCredits and cmdBoughtCredits are the account reads.
const (
	cmdGoatPlan      = `{"success":true,"data":{"planId":"individual-goat","status":"active"}}`
	cmdUnknownPlan   = `{"success":true,"data":{"planId":"individual-enterprise","status":"active"}}`
	cmdNoCredits     = `{"credits":{"monthlyCredits":61.48,"purchasedCredits":0,"freeCredits":0}}`
	cmdBoughtCredits = `{"credits":{"monthlyCredits":0,"purchasedCredits":5,"freeCredits":0}}`
)

// cmdAPIStub is a Command Code API answering the three reads the plan
// filter makes: the vendor's list, the client catalog, and the account's
// plan and credits. Either of the last two can be made to refuse.
type cmdAPIStub struct {
	plan, credits                      string
	planDown, creditsDown, catalogDown bool
}

func (s *cmdAPIStub) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/provider/v1/models":
			if r.URL.Query().Get("view") == "client" {
				if s.catalogDown {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Write([]byte(cmdClientListReply))
				return
			}
			w.Write([]byte(cmdPlainListReply))
		case "/alpha/billing/subscriptions":
			if s.planDown {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(s.plan))
		case "/alpha/billing/credits":
			if s.creditsDown {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(s.credits))
		default:
			t.Errorf("asked %s%s", r.Header.Get("X-Host"), r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })
}

// cmdAPIModels is what the keyed preset's own fetch ends with.
func cmdAPIModels(t *testing.T, key string) []string {
	t.Helper()
	p := Provider{ID: "commandcode", Key: key, Chat: "https://api.commandcode.ai/provider/v1"}
	l, err := p.fetchOne(context.Background())
	if err != nil {
		t.Fatalf("fetchOne(%s): %v", key, err)
	}
	return modelIDs(l.models)
}

// The ladder's ids run into each other: "individual-go" starts both
// "individual-go-v1" and "individual-goat", and "individual-pro" both
// "individual-pro-v1" and "individual-provider". The longest match is the
// plan id, not the first one that fits, and an id carrying a period of its
// own ("individual-goat-monthly") is still the plan it starts with.
func TestCommandCodePlanRanks(t *testing.T) {
	for id, want := range map[string]int{
		"individual-go": 0, "individual-go-v1": 1, "individual-goat": 2,
		"individual-goat-monthly": 2, "individual_goat": 2, "INDIVIDUAL-GOAT": 2,
		"individual-pro": 3, "individual-pro-v1": 4, "individual-provider": 5,
		"individual-max": 6, "individual-ultra": 7, "teams-pro": 8,
		"individual-enterprise": -1, "": -1,
	} {
		if got := cmdRank(id); got != want {
			t.Errorf("cmdRank(%q) = %d, want %d", id, got, want)
		}
	}
}

// #1417 (northword): Command Code's Provider API lists every model it
// sells, the ones a plan includes beside the ones bought on demand, so a
// GOAT account was offered Claude Sonnet 5 and GPT-6.1 Sol, which both
// refuse it. The client catalog names each model's plan, and the plan
// filter leaves out what the account's plan doesn't include.
func TestCommandCodeModelsFollowThePlan(t *testing.T) {
	isolateFetchHome(t)
	stub := &cmdAPIStub{plan: cmdGoatPlan, credits: cmdNoCredits}
	stub.start(t)

	// a GOAT account is offered its own plan's models and the ones below
	// it, and not the Pro and Max ones
	if got, want := cmdAPIModels(t, "cmd-1417-goat"), []string{"deepseek/deepseek-v4.1-flash", "claude-sonnet-5-5"}; !slices.Equal(got, want) {
		t.Errorf("GOAT key: %v, want %v", got, want)
	}

	// credits bought on top are spent a model at a time: with them
	// nothing is left out
	stub.credits = cmdBoughtCredits
	if got := cmdAPIModels(t, "cmd-1417-bought"); len(got) != 4 {
		t.Errorf("credits bought: %v, want all four", got)
	}

	// a plan that can't be read leaves the vendor's list alone
	stub.credits = cmdNoCredits
	stub.planDown = true
	if got := cmdAPIModels(t, "cmd-1417-noplan"); len(got) != 4 {
		t.Errorf("plan unreadable: %v, want all four", got)
	}

	// and one the ladder doesn't know
	stub.planDown = false
	stub.plan = cmdUnknownPlan
	if got := cmdAPIModels(t, "cmd-1417-unknown"); len(got) != 4 {
		t.Errorf("plan off the ladder: %v, want all four", got)
	}

	// credits that can't be read say nothing about the account either: the
	// CLI reads the plan and the credits in one go and lets everything
	// through when that read fails, and billing/credits does fail. Read as
	// "no credits" it would hide the Pro and Max models from an account
	// that bought some.
	stub.plan = cmdGoatPlan
	stub.creditsDown = true
	if got := cmdAPIModels(t, "cmd-1417-nocredits"); len(got) != 4 {
		t.Errorf("credits unreadable: %v, want all four", got)
	}
}

// A client catalog that can't be read says nothing about which models the
// plan has, so none of them goes: the plan is read, and alone it can't
// name a model to leave out.
func TestCommandCodeModelsStayWhenTheCatalogCannotBeRead(t *testing.T) {
	isolateFetchHome(t)
	stub := &cmdAPIStub{plan: cmdGoatPlan, credits: cmdNoCredits, catalogDown: true}
	stub.start(t)

	if got := cmdAPIModels(t, "cmd-1417-nocatalog"); len(got) != 4 {
		t.Errorf("catalog unreadable: %v, want all four", got)
	}
}
