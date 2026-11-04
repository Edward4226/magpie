package gateway

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

const commandCodeUnsupportedInput = `{"error":{"message":"input items may be messages, function calls, function call outputs and reasoning. Item references need stored responses, which are not kept.","type":"invalid_request_error","param":"input","code":"unsupported_input_item"}}`

func TestUnsupportedInputItem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"Command Code", 400, commandCodeUnsupportedInput, true},
		{"item param", 400, `{"error":{"param":"input[3]","code":"unsupported_input_item"}}`, true},
		{"another code", 400, `{"error":{"param":"input","code":"invalid_request_error"}}`, false},
		{"another param", 400, `{"error":{"param":"reasoning.effort","code":"unsupported_input_item"}}`, false},
		{"another status", 429, commandCodeUnsupportedInput, false},
	} {
		if got := unsupportedInputItem(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

// Command Code refuses an item reference because it keeps no stored
// responses. A summary request carrying one is asked again as text, and
// the plain request's summary reaches Codex.
func TestCodexCompactUnsupportedInputItemAsksAgainAsText(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_p","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), `"type":"item_reference"`) {
			return 400, commandCodeUnsupportedInput
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)

	code, body := codexPost(t, `{"model":"fake/m1","stream":true,"store":false,
	  "input":[{"type":"item_reference","id":"msg_1"},
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
	  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"I fixed main.go"}]},
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"now add a test"}]},
	  {"type":"compaction_trigger"}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if got := compactedText(t, body); got != "SUMMARY" {
		t.Errorf("summary %q", got)
	}
	if f.calls != 2 || strings.Contains(string(f.got), `"type":"item_reference"`) {
		t.Fatalf("asked %d times; last request: %s", f.calls, f.got)
	}
	for _, want := range []string{"fix the login bug", "now add a test", "I fixed main.go", "CONTEXT CHECKPOINT COMPACTION"} {
		if !strings.Contains(string(f.got), want) {
			t.Errorf("plain request lacks %q: %s", want, f.got)
		}
	}
}
