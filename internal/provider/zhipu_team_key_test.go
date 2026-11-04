package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/agentenv"
)

// #236: a Zhipu key on a team's GLM Coding Plan, added as a key and not
// through ZCode, has the team's organization and project typed into its
// provider: its windows are asked with type=2 and those two headers. A
// key without them is asked as before, with no team headers.
func TestZhipuKeyTeamFields(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	asked := map[string]http.Header{}
	hosts := map[string][]string{} // where each key's team quota was asked
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.Header.Get("Authorization")
		switch r.Header.Get("X-Host") + r.URL.Path {
		case "open.bigmodel.cn/api/monitor/usage/quota/limit", "bigmodel.cn/api/monitor/usage/quota/limit":
			if r.URL.Query().Get("type") != "2" {
				// a team's key has no plan of its own
				w.Write([]byte(`{"code":500,"msg":"no plan","success":false}`))
				return
			}
			hosts[key] = append(hosts[key], r.Header.Get("X-Host"))
			asked[key] = r.Header.Clone()
			if r.Header.Get("Bigmodel-Organization") != "org-1" || r.Header.Get("Bigmodel-Project") != "proj-1" {
				w.Write([]byte(`{"code":1001,"msg":"no team","success":false}`))
				return
			}
			w.Write([]byte(`{"code":200,"success":true,"data":{"limits":[
				{"type":"CREDIT_LIMIT","unit":3,"percentage":42,"currentValue":420,"usage":1000,"nextResetTime":1790000000000},
				{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":10,"nextResetTime":1790500000000}]}}`))
		default:
			t.Errorf("asked %s%s with %q", r.Header.Get("X-Host"), r.URL.Path, key)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	forget := func() {
		planQuotaCache.Lock()
		planQuotaCache.data = nil
		planQuotaCache.Unlock()
	}
	forget()
	t.Cleanup(forget)

	for _, p := range []Provider{
		{ID: "team", Name: "Team", Chat: "https://open.bigmodel.cn/api/coding/paas/v4", Key: "team-k", ZhipuTeam: &ZhipuTeam{" org-1 ", "proj-1\n"}},
		{ID: "bare", Name: "Bare", Chat: "https://open.bigmodel.cn/api/coding/paas/v4", Key: "bare-k"},
		{ID: "blank", Name: "Blank", Chat: "https://open.bigmodel.cn/api/paas/v4", Key: "blank-k", ZhipuTeam: &ZhipuTeam{" ", ""}},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := Find("team"); p.ZhipuTeam == nil || *p.ZhipuTeam != (ZhipuTeam{"org-1", "proj-1"}) {
		t.Fatalf("kept: %+v", p.ZhipuTeam)
	}
	if p, _ := Find("blank"); p.ZhipuTeam != nil {
		t.Fatalf("blank kept: %+v", p.ZhipuTeam)
	}
	if p, _ := Find("team"); !TakesZhipuTeam(*p) {
		t.Fatal("a Zhipu key takes a team")
	}
	if TakesZhipuTeam(Provider{Chat: "https://api.z.ai/api/coding/paas/v4"}) != true ||
		TakesZhipuTeam(Provider{Chat: "https://api.deepseek.com/v1"}) {
		t.Fatal("takes a team: Z.ai does, DeepSeek doesn't")
	}

	got := map[string]SubscriptionQuota{}
	for _, q := range PlanQuotas(context.Background()) {
		got[q.Provider] = q
	}
	mu.Lock()
	defer mu.Unlock()
	q, ok := got["team"]
	if !ok || q.Error != "" || q.Plan != "GLM Coding Team" || len(q.Windows) != 2 ||
		q.Windows[0].Name != "5 hours" || q.Windows[0].Used != 42 || q.Windows[0].Span != 5*time.Hour ||
		q.Windows[1].Name != "7 days" || q.Windows[1].Used != 10 {
		t.Fatalf("team: %+v", q)
	}
	if hd := asked["team-k"]; hd.Get("Bigmodel-Organization") != "org-1" || hd.Get("Bigmodel-Project") != "proj-1" || hd.Get("Set-Language") != "zh" {
		t.Fatalf("team asked with %v", hd)
	}
	// at the key's own host, as CC Switch asks it; bigmodel.cn only when
	// that one has no windows
	if h := strings.Join(hosts["team-k"], ","); h != "open.bigmodel.cn" {
		t.Fatalf("team asked at %s", h)
	}
	if h := strings.Join(hosts["bare-k"], ","); h != "open.bigmodel.cn,bigmodel.cn" {
		t.Fatalf("bare asked at %s", h)
	}
	// without them nothing changes: asked with no team headers, no windows
	if hd, ok := asked["bare-k"]; !ok || hd.Get("Bigmodel-Organization") != "" || hd.Get("Bigmodel-Project") != "" {
		t.Fatalf("bare asked with %v (%v)", hd, ok)
	}
	if q := got["bare"]; len(q.Windows) != 0 || q.Error == "" {
		t.Fatalf("bare: %+v", q)
	}
	if _, ok := got["blank"]; ok {
		t.Fatalf("a pay-as-you-go key with no team has no card: %+v", got["blank"])
	}
}

// #3442: a GLM account can hold its member's own plan and a team seat at
// once. When the provider names the team, the team's windows are the
// key's card even though the member's own plan also answers; the own
// plan's endpoint is not asked at all.
func TestZhipuTeamBeforeTheMembersOwnPlan(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	var mu sync.Mutex
	personal, team := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			t.Errorf("asked %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("type") == "2" {
			team++
			if r.Header.Get("Bigmodel-Organization") != "org-1" || r.Header.Get("Bigmodel-Project") != "proj-1" {
				t.Errorf("team asked with %v", r.Header)
			}
			w.Write([]byte(`{"code":200,"success":true,"data":{"limits":[
				{"type":"CREDIT_LIMIT","unit":3,"percentage":42,"nextResetTime":1790000000000},
				{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":10,"nextResetTime":1790500000000}]}}`))
			return
		}
		// the member's own plan: the old order preferred it
		personal++
		w.Write([]byte(`{"success":true,"data":{"level":"pro","limits":[
			{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":12,"nextResetTime":1790000000000}]}}`))
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	if err := Save(Provider{ID: "mixed", Name: "Mixed", Chat: "https://open.bigmodel.cn/api/coding/paas/v4",
		Key: "mixed-k", ZhipuTeam: &ZhipuTeam{"org-1", "proj-1"}}); err != nil {
		t.Fatal(err)
	}
	got := map[string]SubscriptionQuota{}
	for _, q := range PlanQuotas(context.Background()) {
		got[q.Provider] = q
	}
	mu.Lock()
	defer mu.Unlock()
	q, ok := got["mixed"]
	if !ok || q.Error != "" || q.Plan != "GLM Coding Team" || len(q.Windows) != 2 ||
		q.Windows[0].Name != "5 hours" || q.Windows[0].Used != 42 ||
		q.Windows[1].Name != "7 days" || q.Windows[1].Used != 10 {
		t.Fatalf("the team's card: %+v", q)
	}
	if personal != 0 {
		t.Fatalf("the member's own plan was asked %d times before the team's", personal)
	}
	if team != 1 {
		t.Fatalf("team asked %d times", team)
	}
}

// #3442: the provider's team fields decide which plan a key's windows
// are read from first. The member's own plan is still read when the team
// doesn't serve the key, and then is not asked as a team's again; a key
// its own plan refuses keeps that refusal.
func TestZhipuPlanKeyTeamPrecedence(t *testing.T) {
	teamWindows := `{"code":200,"success":true,"data":{"limits":[
		{"type":"CREDIT_LIMIT","unit":3,"percentage":42,"nextResetTime":1790000000000},
		{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":10,"nextResetTime":1790500000000}]}}`
	ownPlan := `{"success":true,"data":{"level":"pro","limits":[
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":12,"nextResetTime":1790000000000}]}}`
	teamRefuses := `{"code":500,"msg":"no team","success":false}`
	teamEmpty := `{"success":true,"data":{"limits":[]}}`
	for _, c := range []struct {
		name      string
		team      *ZhipuTeam
		teamReply string
		ownReply  string
		wantPlan  string
		wantTeam  bool
		wantUsed  []float64
		wantErr   string
		wantOwn   int
		wantTeams int // -1 for at least one
	}{
		{name: "team first, own plan also answers", team: &ZhipuTeam{"org-1", "proj-1"},
			teamReply: teamWindows, ownReply: ownPlan,
			wantPlan: "GLM Coding Team", wantTeam: true, wantUsed: []float64{42, 10}, wantOwn: 0, wantTeams: 1},
		{name: "team rejects the key, own plan used", team: &ZhipuTeam{"org-1", "proj-1"},
			teamReply: teamRefuses, ownReply: ownPlan,
			wantPlan: "pro", wantUsed: []float64{12}, wantOwn: 1, wantTeams: -1},
		{name: "team has no windows, own plan used", team: &ZhipuTeam{"org-1", "proj-1"},
			teamReply: teamEmpty, ownReply: ownPlan,
			wantPlan: "pro", wantUsed: []float64{12}, wantOwn: 1, wantTeams: -1},
		{name: "no team, own plan first", ownReply: ownPlan,
			wantPlan: "pro", wantUsed: []float64{12}, wantOwn: 1, wantTeams: 0},
		{name: "team configured, own plan refuses the key", team: &ZhipuTeam{"org-1", "proj-1"},
			teamReply: teamRefuses, ownReply: zhipuEnvelope1000,
			wantErr: "Zhipu didn't take this key", wantOwn: 1, wantTeams: -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var events []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Path != "/api/monitor/usage/quota/limit" {
					events = append(events, "other:"+r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.URL.Query().Get("type") == "2" {
					events = append(events, "team")
					w.Write([]byte(c.teamReply))
					return
				}
				events = append(events, "own")
				w.Write([]byte(c.ownReply))
			}))
			defer srv.Close()
			old := http.DefaultClient.Transport
			http.DefaultClient.Transport = rewrite{srv}
			t.Cleanup(func() { http.DefaultClient.Transport = old })

			p := Provider{ID: "glm", Chat: "https://open.bigmodel.cn/api/coding/paas/v4", Key: "k", ZhipuTeam: c.team}
			src, ok := planQuotaSourceOf(p)
			if !ok {
				t.Fatal("no plan source")
			}
			plan, ws, team, err := planKeyWindows(context.Background(), p, src, p.Key)
			mu.Lock()
			defer mu.Unlock()
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error %v, want %q", err, c.wantErr)
				}
			} else if err != nil {
				t.Fatalf("error: %v", err)
			}
			if plan != c.wantPlan || team != c.wantTeam {
				t.Fatalf("plan %q team %v, want %q %v", plan, team, c.wantPlan, c.wantTeam)
			}
			if c.wantErr == "" {
				var used []float64
				for _, w := range ws {
					used = append(used, w.Used)
				}
				if !slices.Equal(used, c.wantUsed) {
					t.Fatalf("windows %+v, want used %v", ws, c.wantUsed)
				}
			}
			own, teams, lastOwn := 0, 0, -1
			for i, e := range events {
				switch e {
				case "own":
					own, lastOwn = own+1, i
				case "team":
					teams++
					if lastOwn >= 0 {
						t.Errorf("team asked again after the own plan: %v", events)
					}
				}
			}
			if own != c.wantOwn || c.wantTeams >= 0 && teams != c.wantTeams || c.wantTeams < 0 && teams == 0 {
				t.Fatalf("asked own %d team %d: %v", own, teams, events)
			}
		})
	}
}
