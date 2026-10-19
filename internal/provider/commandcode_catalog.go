package provider

// Command Code's Provider API lists every model it sells, the ones a plan
// includes beside the ones bought on demand (#1417): a GOAT account is
// offered Claude Sonnet 5 and GPT-6.1 Sol, and both refuse the request
// ("MODEL_NOT_IN_PLAN: … available in Pro and above plans or extra on
// demand usage"), so the keyed preset's model picker offered 25 models of
// its 93 that could only fail. The same host has a list for its own
// clients (?view=client) which names each model's plan; read beside the
// key's own plan it says which of the vendor's models the account has.
//
// The rules are Command Code's CLI's (1.84.0, its rankedAccess and
// evaluateModelAccess), and they fail open: a plan that can't be read, a
// catalog that can't be read, or a model the catalog doesn't name leaves
// the list as it was. Hiding a model the account can use is worse than
// showing one it can't: the one it can't says why, the one that is gone
// says nothing.

import (
	"context"
	"strings"

	"github.com/yetone/magpie/internal/catalog"
)

// cmdClientCatalogURL is Command Code's client catalog: every model with
// the plan it needs. Read anonymously answers it too, but the key goes
// with it as every other request does.
var cmdClientCatalogURL = cmdAPI + "/provider/v1/models?view=client"

// cmdLadder is what the plans are ranked on, weakest first, as Command
// Code's CLI ranks them: a plan includes what every plan below it does.
var cmdLadder = []string{"individual-go", "individual-go-v1", "individual-goat", "individual-pro",
	"individual-pro-v1", "individual-provider", "individual-max", "individual-ultra", "teams-pro"}

// cmdRank is a plan id's place on the ladder, -1 when it is not one of
// them. The longest match wins: "individual-go" starts
// "individual-go-v1" and "individual-goat", and "individual-pro" starts
// "individual-pro-v1" and "individual-provider". The id often carries a
// period of its own ("individual-goat-monthly"), which is ignored.
func cmdRank(id string) int {
	id = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(id)), "_", "-")
	rank, longest := -1, 0
	for i, p := range cmdLadder {
		if strings.HasPrefix(id, p) && len(p) > longest {
			rank, longest = i, len(p)
		}
	}
	return rank
}

// cmdAPIPreset says whether this provider is at api.commandcode.ai: the
// keyed preset ("commandcode"), whose key comes from Studio, which is the
// one offered the vendor's whole list.
func cmdAPIPreset(p Provider) bool {
	for _, base := range []string{p.Chat, p.Anthropic, p.Responses} {
		if hostOf(base) == "api.commandcode.ai" {
			return true
		}
	}
	return false
}

// cmdModelPlans reads the client catalog: the plan every model needs. ok
// is false when it can't be read, which leaves the list alone.
func cmdModelPlans(ctx context.Context, key string) (map[string]string, bool) {
	var r struct {
		Data []struct {
			ID          string  `json:"id"`
			MinimumPlan *string `json:"minimumPlan"`
		} `json:"data"`
	}
	if err := accountJSON(ctx, cmdClientCatalogURL, key, nil, &r); err != nil || len(r.Data) == 0 {
		return nil, false
	}
	plans := make(map[string]string, len(r.Data))
	for _, m := range r.Data {
		if m.ID == "" || m.MinimumPlan == nil || *m.MinimumPlan == "" {
			continue
		}
		plans[m.ID] = *m.MinimumPlan
	}
	return plans, true
}

// cmdAnyCredits says whether the account bought, or was given, credits
// beside its plan: with those, the models the plan leaves out are bought
// one by one and none of them is refused, as the CLI's evaluateModelAccess
// reads it. ok is false when the reply can't be read, which is not the
// same as a reply saying there are none: the CLI reads the plan and the
// credits in one go and lets everything through when that read fails, and
// this endpoint does fail (billing/credits 503s, see planquota.go). Left
// as ok=false the caller keeps the list, so an account that bought credits
// is never shown fewer models than one that didn't.
func cmdAnyCredits(ctx context.Context, key string) (any, ok bool) {
	var c cmdCredits
	if err := accountJSON(ctx, cmdAPI+"/alpha/billing/credits", key, nil, &c); err != nil {
		return false, false
	}
	bought, hasBought := number(c.Credits.Purchased)
	free, hasFree := number(c.Credits.Free)
	return hasBought && bought > 0 || hasFree && free > 0, true
}

// cmdKeepPlan leaves out of Command Code's own list the models the
// account's plan doesn't include (#1417): what the vendor lists and what
// the plan pays for are two lists, and only the client catalog's says
// where they part. Everything read for it can fail without taking the
// list away — another provider is not asked, a key with no plan (pay as
// you go), a plan, catalog or credits reply that can't be read, a model
// the catalog doesn't name, and a plan id that is not on the ladder all
// leave the models as they came.
func cmdKeepPlan(ctx context.Context, p Provider, ms []catalog.Model) []catalog.Model {
	if len(ms) == 0 || strings.TrimSpace(p.Key) == "" || !cmdAPIPreset(p) {
		return ms
	}
	plans, ok := cmdModelPlans(ctx, p.Key)
	if !ok {
		return ms
	}
	rank := cmdRank(cmdKeyPlanID(ctx, p.Key))
	if rank < 0 {
		return ms
	}
	credits, known := cmdAnyCredits(ctx, p.Key)
	if !known || credits {
		return ms
	}
	out := make([]catalog.Model, 0, len(ms))
	for _, m := range ms {
		need, listed := plans[m.ID]
		if r := cmdRank(need); !listed || r < 0 || rank >= r {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		// a plan that includes none of the vendor's models is not a plan
		// to trust this reading of: a catalog that moved under it, or a
		// ladder that reads the id wrong
		return ms
	}
	return out
}
