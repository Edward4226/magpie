package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// mistralRefuseTranslatedReasoning answers as the reporter's Mistral did:
// a message field named reasoning_content (or reasoning, or
// reasoning_details) is 422 extra_forbidden.
func mistralRefuseTranslatedReasoning(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	for i, m := range q.Messages {
		var role string
		json.Unmarshal(m["role"], &role)
		for _, f := range messageReasoning {
			if _, ok := m[f]; ok {
				return 422, fmt.Sprintf(`{"detail":[{"type":"extra_forbidden","loc":["body","messages",%d,%q,%q],"msg":"Extra inputs are not permitted","input":%s}]}`, i, role, f, m[f])
			}
		}
	}
	return 0, ""
}

// A Responses request translated to Mistral must not send an earlier
// assistant turn's thinking as reasoning_content: Mistral turns that field
// away with 422 extra_forbidden (#494). The field belongs only to models
// whose upstream asks it back, and Mistral takes the reasoning as its
// typed thinking content part instead (#4906).
func TestTranslatedMistralReasoningAsThinking(t *testing.T) {
	for _, c := range []struct {
		name    string
		model   string
		preset  string
		refuse  func([]byte) (int, string)
		replays bool
	}{
		{"mistral deepseek", "deepseek-v4-1-flash-1017", "mistral", mistralRefuseTranslatedReasoning, false},
		{"deepseek elsewhere", "deepseek-v4.1-flash", "", deepseekRefuseToolTurns, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{t: t, reply: chatStreamOK, refuse: c.refuse}
			up := setup(t, provider.Chat, f)
			chatProvider(t, up.URL, provider.Provider{ID: "p", Preset: c.preset, Models: []string{c.model}})
			code, body := postTo(t, New(), "/v1/responses", `{
				"model":"p/`+c.model+`",
				"stream":false,
				"max_output_tokens":40,
				"tools":[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}],
				"input":[
					{"type":"message","role":"user","content":[{"type":"input_text","text":"Read note.txt and tell me the word."}]},
					{"type":"reasoning","summary":[{"type":"summary_text","text":"I should read note.txt with the tool."}]},
					{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"note.txt\"}"},
					{"type":"function_call_output","call_id":"call_1","output":"pelican"}
				]
			}`)
			if code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls: %s", code, f.calls, body)
			}
			if got := strings.Contains(string(f.got), `"reasoning_content"`); got != c.replays {
				t.Fatalf("reasoning_content present = %v, want %v: %s", got, c.replays, f.got)
			}
			if got := strings.Contains(string(f.got), `"type":"thinking"`); got == c.replays {
				t.Fatalf("thinking part present = %v, want %v: %s", got, !c.replays, f.got)
			}
		})
	}
}
