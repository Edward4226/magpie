package gateway

import (
	"encoding/json"
	"testing"
)

// Command Code's stealth and reflection models refuse a replayed assistant
// turn with no reasoning_content: "Missing required parameter:
// 'messages[1].reasoning_content'" (#2741, m002346). The replay was keyed
// on the plugin's id, which a moved account shows; the built-in provider's
// host (api.commandcode.ai) answered nothing of the kind and got its turns
// replayed bare - first turn 200, every turn after 400.
func TestCommandCodeHostReplaysReasoning(t *testing.T) {
	for _, host := range []string{"api.commandcode.ai", "commandcode-plan"} {
		if !chatReplaysReasoning(host, "stealth/cat-jam:free") {
			t.Errorf("%s: the replay is off, and the stealth model 400s on turn two", host)
		}
	}
	// a host that never asked for it is unchanged
	if chatReplaysReasoning("api.openai.com", "gpt") {
		t.Error("openai's own now replays reasoning")
	}
}

// The replayed assistant tool turn carries the reasoning it was given: the
// incoming chat history said it as reasoning_content (Responses->Chat
// translation keeps it as a thinking part, #388's shape), and the built-in
// Command Code's host, like its plugin, has it back on the tool turn.
func TestCommandCodeReplayCarriesReasoningContent(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}},
		{Role: "assistant", Parts: []Part{{Kind: Thinking, Text: "THINK"}, {Kind: ToolCall, ID: "c1", Name: "sh", Args: json.RawMessage("{}")}}},
		{Role: "tool", Parts: []Part{{Kind: Text, Text: "ok"}, {Kind: ToolResult, CallID: "c1", Text: "ok"}}},
	}}
	b := buildChat(r, "stealth/cat-jam:free", "api.commandcode.ai", false)
	var q struct {
		Messages []struct {
			Role             string `json:"role"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range q.Messages {
		if m.Role == "assistant" {
			if m.ReasoningContent != "THINK" {
				t.Fatalf("assistant turn replayed without its reasoning: %s", b)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no assistant turn in %s", b)
	}
}
