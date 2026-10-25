package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// volcAllowanceServer answers GetCodingPlanUsage with future windows and
// counts the asks. While down it answers a failure, as the control plane
// does for a while.
func volcAllowanceServer(t *testing.T, down *atomic.Bool, asks *atomic.Int32) (month time.Time) {
	t.Helper()
	now := time.Now()
	month = now.Add(20 * 24 * time.Hour).Truncate(time.Second)
	week := now.Add(4 * 24 * time.Hour).Truncate(time.Second)
	five := now.Add(2 * time.Hour).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Host") != volcOpenAPIHost || r.URL.Query().Get("Action") != "GetCodingPlanUsage" {
			t.Errorf("asked %s %s%s", r.Method, r.Header.Get("X-Host"), r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Error(err)
		}
		asks.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Status":"ok","QuotaUsage":[
			{"Level":"session","Percent":25,"ResetTimestamp":%d},
			{"Level":"weekly","Percent":40,"ResetTimestamp":%d},
			{"Level":"monthly","Percent":100,"ResetTimestamp":%d}]}}`,
			five.Unix(), week.Unix(), month.Unix())
	}))
	t.Cleanup(srv.Close)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	return month
}

func volcCodingProvider(id, key string) Provider {
	return Provider{
		ID:              id,
		Name:            "Volcengine Ark",
		Chat:            "https://ark.cn-beijing.volces.com/api/coding/v3",
		Key:             key,
		AccessKeyID:     "AK1",
		SecretAccessKey: "SK1",
	}
}

// The Usage card's successful control-plane reading is also routing's
// allowance when the provider has one inference key, and that card stands
// in after a restart while the control plane cannot be read.
func TestVolcKeyAllowanceFromItsPlanCard(t *testing.T) {
	keyLimitsHome(t)
	var down atomic.Bool
	var asks atomic.Int32
	month := volcAllowanceServer(t, &down, &asks)
	p := volcCodingProvider("volcengine", "ark-a")
	if err := Save(p); err != nil {
		t.Fatal(err)
	}

	qs := PlanQuotas(context.Background())
	if len(qs) != 1 || qs[0].Provider != p.ID || qs[0].Error != "" || len(qs[0].Windows) != 3 {
		t.Fatalf("cards: %+v", qs)
	}
	a, ok := KeyAllowance(p)
	used, renews := a.For("", time.Now())
	if !ok || used != 100 || len(renews) != 3 || !renews[0].Equal(month) {
		t.Fatalf("after the card's read: %v %+v", ok, a)
	}
	if n := asks.Load(); n != 1 {
		t.Fatalf("routing asked the control plane again after the card: %d asks", n)
	}

	// Restarted with the control plane down: the card on disk stands in,
	// and the failed background read leaves it standing.
	down.Store(true)
	keyAllowances.Lock()
	keyAllowances.m = nil
	keyAllowances.Unlock()
	lastQuotas.Lock()
	lastQuotas.m, lastQuotas.loaded = nil, false
	lastQuotas.Unlock()
	a, ok = KeyAllowance(p)
	if used, _ := a.For("", time.Now()); !ok || used != 100 {
		t.Fatalf("from the card on disk: %v %+v", ok, a)
	}
	waitRead(t)
	if a, ok := KeyAllowance(p); !ok || len(a) != 3 {
		t.Fatalf("after a failed read: %v %+v", ok, a)
	}
}

// Routing reads the same control-plane action in the background for a
// one-key Ark provider, without waiting for it (KeyAllowance).
func TestVolcKeyAllowanceReadBehindRouting(t *testing.T) {
	keyLimitsHome(t)
	var down atomic.Bool
	var asks atomic.Int32
	volcAllowanceServer(t, &down, &asks)
	p := volcCodingProvider("volcengine", "ark-a")
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if a, ok := KeyAllowance(p); ok || a != nil {
		t.Fatalf("known before it was read: %+v", a)
	}
	a := waitAllowance(t, p, func(Allowance) bool { return true })
	if used, _ := a.For("", time.Now()); used != 100 {
		t.Fatalf("used %v of %+v", used, a)
	}
	if n := asks.Load(); n != 1 {
		t.Fatalf("asked %d times", n)
	}
}

// One control-plane account and plan endpoint cannot say which of several
// inference keys is on that account, so their windows stay unknown rather
// than being fanned out to every key of the provider (#2849).
func TestVolcKeyAllowanceLeavesMultiKeyProviderUnknown(t *testing.T) {
	keyLimitsHome(t)
	var down atomic.Bool
	var asks atomic.Int32
	volcAllowanceServer(t, &down, &asks)
	p := volcCodingProvider("volcengine", "ark-a")
	p.Keys = []KeyAccount{{Name: "second", Key: "ark-b"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if qs := PlanQuotas(context.Background()); len(qs) != 1 || len(qs[0].Windows) != 3 {
		t.Fatalf("cards: %+v", qs)
	}
	other := p
	other.Key = "ark-b"
	for _, k := range []Provider{p, other} {
		if KeyAllowanceID(k) != "" {
			t.Errorf("%s's key reads windows", k.Key)
		}
		if a, ok := KeyAllowance(k); ok || a != nil {
			t.Errorf("%s's key is known: %+v", k.Key, a)
		}
	}
	keyAllowances.Lock()
	n := len(keyAllowances.m)
	keyAllowances.Unlock()
	if n != 0 {
		t.Fatalf("%d key allowances noted for a provider with several keys", n)
	}
}

// One access key and plan card is named for one provider. Another provider
// configured with the same access key isn't handed that card's allowance
// as if its inference key had been identified.
func TestVolcKeyAllowanceNotFannedOutByAccessKey(t *testing.T) {
	keyLimitsHome(t)
	var down atomic.Bool
	var asks atomic.Int32
	volcAllowanceServer(t, &down, &asks)
	first := volcCodingProvider("volc-first", "ark-a")
	second := volcCodingProvider("volc-second", "ark-b")
	for _, p := range []Provider{first, second} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
	qs := PlanQuotas(context.Background())
	if len(qs) != 1 || qs[0].Provider != first.ID {
		t.Fatalf("cards: %+v", qs)
	}
	keyAllowances.Lock()
	_, firstNoted := keyAllowances.m[keyAllowanceID(first)]
	_, secondNoted := keyAllowances.m[keyAllowanceID(second)]
	keyAllowances.Unlock()
	if !firstNoted || secondNoted {
		t.Fatalf("noted first=%v second=%v", firstNoted, secondNoted)
	}
}

// The key's allowance cache is also told apart by the control-plane
// account, plan and region that produced it, so changing one cannot leave
// another account's or plan's windows under the same inference key.
func TestVolcKeyAllowanceIdentityIncludesControlPlane(t *testing.T) {
	base := volcCodingProvider("volcengine", "ark-a")
	otherKey := base
	otherKey.AccessKeyID = "AK2"
	otherPlan := base
	otherPlan.Chat = "https://ark.cn-beijing.volces.com/api/plan/v3"
	otherRegion := base
	otherRegion.Chat = "https://ark.cn-shanghai.volces.com/api/coding/v3"
	seen := map[string]bool{}
	for _, p := range []Provider{base, otherKey, otherPlan, otherRegion} {
		id := keyAllowanceID(p)
		if seen[id] {
			t.Fatalf("two control-plane sources share allowance id %q", id)
		}
		seen[id] = true
	}
}
