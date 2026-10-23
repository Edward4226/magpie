package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

type accessProgramsUpstream struct {
	mu    sync.Mutex
	paths []string
	bodys [][]byte
}

func (u *accessProgramsUpstream) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.paths = append(u.paths, r.URL.Path)
	u.bodys = append(u.bodys, body)
	u.mu.Unlock()
	reply := sse(
		`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","model":"m","status":"in_progress"}}`,
		`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"OK"}`,
		`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","model":"m","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`,
	)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(reply)),
		Request:    r,
	}, nil
}

func (u *accessProgramsUpstream) last(t *testing.T) (string, []byte) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.bodys) == 0 {
		t.Fatal("the upstream was asked nothing")
	}
	return u.paths[len(u.paths)-1], u.bodys[len(u.bodys)-1]
}

// #3172: Codex's access_programs reaches Responses backends except OpenCode
// Go, which refuses the whole request with a misleading 503. The exception
// is provider-scoped on both the relayed and translated paths.
func TestOpenCodeGoDropsAccessPrograms(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const programs = `{"cyber":"standard"}`

	up := &accessProgramsUpstream{}

	if err := provider.Save(provider.Provider{
		ID: "ocgo", Name: "OpenCode Go", Key: "k",
		Chat: "https://opencode.ai/zen/go/v1", Responses: "https://opencode.ai/zen/go/v1",
		Models: []string{"deepseek-v4.1-flash", "glm-5.3-flash"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("ocgo/deepseek-v4.1-flash", "responses"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID: "ocgo-extra", Name: "OpenCode Go with extraBody", Key: "k",
		Chat: "https://opencode.ai/zen/go/v1", Responses: "https://opencode.ai/zen/go/v1",
		Models:    []string{"deepseek-extra"},
		ExtraBody: provider.ExtraBody{"access_programs": json.RawMessage(programs)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("ocgo-extra/deepseek-extra", "responses"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID: "oczen", Name: "OpenCode Zen", Key: "k",
		Chat: "https://opencode.ai/zen/v1", Responses: "https://opencode.ai/zen/v1",
		Models: []string{"deepseek-zen"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("oczen/deepseek-zen", "responses"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID: "other", Name: "Other", Key: "k", Responses: "https://other.test/v1",
		Models: []string{"deepseek-other"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("other/deepseek-other", "responses"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID: "mixed", Name: "Mixed", Key: "k",
		Chat: "https://opencode.ai/zen/go/v1", Responses: "https://proxy.test/v1",
		Models: []string{"deepseek-mixed"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("mixed/deepseek-mixed", "responses"); err != nil {
		t.Fatal(err)
	}

	s := New()
	s.client = &http.Client{Transport: up}
	send := func(path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	for _, c := range []struct {
		name, path, body string
		want             bool
	}{
		{
			name: "responses relay",
			path: "/v1/responses",
			body: `{"model":"ocgo/deepseek-v4.1-flash","input":"Reply with OK.","stream":true,"access_programs":` + programs + `}`,
		},
		{
			name: "chat translated to responses",
			path: "/v1/chat/completions",
			body: `{"model":"ocgo/deepseek-v4.1-flash","messages":[{"role":"user","content":"Reply with OK."}],"stream":true,"access_programs":` + programs + `}`,
		},
		{
			name: "same provider other model",
			path: "/v1/responses",
			body: `{"model":"ocgo/glm-5.3-flash","input":"Reply with OK.","stream":true,"access_programs":` + programs + `}`,
		},
		{
			name: "extraBody added after the request",
			path: "/v1/responses",
			body: `{"model":"ocgo-extra/deepseek-extra","input":"Reply with OK.","stream":true}`,
		},
		{
			name: "opencode zen deepseek",
			path: "/v1/responses",
			body: `{"model":"oczen/deepseek-zen","input":"Reply with OK.","stream":true,"access_programs":` + programs + `}`,
			want: true,
		},
		{
			name: "other provider deepseek",
			path: "/v1/responses",
			body: `{"model":"other/deepseek-other","input":"Reply with OK.","stream":true,"access_programs":` + programs + `}`,
			want: true,
		},
		{
			name: "proxy endpoint beside go",
			path: "/v1/responses",
			body: `{"model":"mixed/deepseek-mixed","input":"Reply with OK.","stream":true,"access_programs":` + programs + `}`,
			want: true,
		},
		{
			name: "proxy endpoint on translated path",
			path: "/v1/chat/completions",
			body: `{"model":"mixed/deepseek-mixed","messages":[{"role":"user","content":"Reply with OK."}],"stream":true,"access_programs":` + programs + `}`,
			want: true,
		},
		{
			name: "null is dropped too",
			path: "/v1/responses",
			body: `{"model":"ocgo/deepseek-v4.1-flash","input":"Reply with OK.","stream":true,"access_programs":null}`,
		},
	} {
		code, out := send(c.path, c.body)
		if code != http.StatusOK {
			t.Errorf("%s: status %d: %s", c.name, code, out)
			continue
		}
		gotPath, got := up.last(t)
		if !strings.HasSuffix(gotPath, "/responses") {
			t.Errorf("%s: upstream path %q", c.name, gotPath)
		}
		ap := gjson.GetBytes(got, "access_programs")
		if ap.Exists() != c.want {
			t.Errorf("%s: access_programs exists=%v, want %v in %s", c.name, ap.Exists(), c.want, got)
		}
		if c.want && ap.Raw != programs {
			t.Errorf("%s: access_programs %q, want %q", c.name, ap.Raw, programs)
		}
	}
}

func TestWithoutTopMember(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
	}{
		{"middle", `{"model":"m", "access_programs": {}, "input":"hi"}`, `{"model":"m", "input":"hi"}`},
		{"last", `{"model":"m", "access_programs": null}`, `{"model":"m"}`},
		{"only", `{"access_programs":{"cyber":"standard"}}`, `{}`},
		{"duplicates", `{"access_programs":1,"x":2,"access_programs":3,"y":4}`, `{"x":2,"y":4}`},
		{"outer whitespace", "\n {\"model\":\"m\", \"access_programs\":{}}\n", "\n {\"model\":\"m\"}\n"},
		{"absent", `{"model":"m"}`, `{"model":"m"}`},
		{"not an object", `[]`, `[]`},
		{"malformed", `{"model":"m", "access_programs":`, `{"model":"m", "access_programs":`},
	} {
		if got := string(withoutTopMember([]byte(c.body), "access_programs")); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(withoutTopMember([]byte(`{"model":"m","access_programs":1,"input":"hi"}`), "access_programs"), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["access_programs"]; ok || string(m["model"]) != `"m"` || string(m["input"]) != `"hi"` {
		t.Fatalf("rewritten body: %v", m)
	}
}
