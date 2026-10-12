package gateway

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// notice is the item Codex writes between one batch's tool results when the
// image a tool returned was resized for the model.
const resizeNotice = `<image_resize_notice>
Image 1 of 1 in the preceding tool output was resized from 1024x9000 to 233x2048 pixels.
</image_resize_notice>`

// orderOf is a Responses request's input items as "type:call_id" strings.
func orderOf(t *testing.T, body []byte) []string {
	t.Helper()
	var order []string
	for _, it := range inputOf(t, body) {
		typ, _ := it["type"].(string)
		id, _ := it["call_id"].(string)
		order = append(order, typ+":"+id)
	}
	return order
}

// A batch of two calls whose results have a message between them: the
// message goes after both results, where the run a strict upstream checks
// is whole. Fixtures are the reporter's own bytes — two parallel calls
// (view_image and exec_command), an output holding an image, Codex's resize
// notice, and the second output.
func TestJoinToolRunsMovesAnItemOutOfARun(t *testing.T) {
	body := []byte(`{"model":"m","input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"look"}]},` +
		`{"type":"function_call","call_id":"call_00_hC0DonBjeKHawzABcyLL1642","name":"view_image","arguments":"{\"path\":\"wide.png\"}"},` +
		`{"type":"function_call","call_id":"call_01_mNySew0rARWuQYQXPeIY2319","name":"exec_command","arguments":"{\"cmd\":\"python -c ...\"}"},` +
		`{"type":"function_call_output","call_id":"call_00_hC0DonBjeKHawzABcyLL1642","output":[{"type":"input_image","image_url":"data:image/png;base64,AQID"}]},` +
		`{"type":"message","role":"developer","content":[{"type":"input_text","text":` + jsonStr(resizeNotice) + `}]},` +
		`{"type":"function_call_output","call_id":"call_01_mNySew0rARWuQYQXPeIY2319","output":"PIL 11.3.0\r\n"}]}`)
	original := bytes.Clone(body)
	got := joinToolRuns(body)
	if !bytes.Equal(body, original) {
		t.Fatal("mutated the caller's request buffer")
	}
	want := []string{
		"message:",
		"function_call:call_00_hC0DonBjeKHawzABcyLL1642",
		"function_call:call_01_mNySew0rARWuQYQXPeIY2319",
		"function_call_output:call_00_hC0DonBjeKHawzABcyLL1642",
		"function_call_output:call_01_mNySew0rARWuQYQXPeIY2319",
		"message:",
	}
	order := orderOf(t, got)
	if len(order) != len(want) {
		t.Fatalf("items %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("items %v, want %v", order, want)
		}
	}
	// the notice keeps its text, and the results keep theirs
	if !bytes.Contains(got, []byte("image_resize_notice")) ||
		!bytes.Contains(got, []byte("PIL 11.3.0")) ||
		!bytes.Contains(got, []byte("data:image/png;base64,AQID")) {
		t.Fatalf("lost an item's content: %s", got)
	}
	// the same request again is left as it is
	if again := joinToolRuns(got); !bytes.Equal(again, got) {
		t.Fatalf("not idempotent:\n%s\n%s", got, again)
	}
}

// Only an item that breaks a run moves: one between two whole exchanges, a
// request with no tool items, and anything that isn't a Responses body go
// byte for byte.
func TestJoinToolRunsLeavesWholeExchangesAlone(t *testing.T) {
	for _, keep := range []string{
		// each exchange is whole: the call before the notice is answered,
		// and so is the one after it
		`{"model":"m","input":[` +
			`{"type":"function_call","call_id":"call_a","name":"echo","arguments":"{}"},` +
			`{"type":"function_call_output","call_id":"call_a","output":"a"},` +
			`{"type":"message","role":"developer","content":[{"type":"input_text","text":"a note"}]},` +
			`{"type":"function_call","call_id":"call_b","name":"echo","arguments":"{}"},` +
			`{"type":"function_call_output","call_id":"call_b","output":"b"}]}`,
		// a call nothing answers is not a run to keep together
		`{"model":"m","input":[` +
			`{"type":"function_call","call_id":"call_a","name":"echo","arguments":"{}"},` +
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"never mind"}]}]}`,
		// parallel results already contiguous, and out of the calls' order
		`{"model":"m","input":[` +
			`{"type":"function_call","call_id":"call_a","name":"echo","arguments":"{}"},` +
			`{"type":"function_call","call_id":"call_b","name":"echo","arguments":"{}"},` +
			`{"type":"function_call_output","call_id":"call_b","output":"b"},` +
			`{"type":"function_call_output","call_id":"call_a","output":"a"}]}`,
		`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		`not json`, `{}`, `{"input":"hello"}`,
	} {
		if got := joinToolRuns([]byte(keep)); string(got) != keep {
			t.Fatalf("rewritten:\n%s\nto\n%s", keep, got)
		}
	}
}

// The relay keeps the run whole for the upstream: a request whose batch has
// an item between its results comes out with the results together, and an
// upstream that checks the exchange answers it.
func TestResponsesRelayKeepsAToolRunTogether(t *testing.T) {
	f := &fake{t: t, reply: sse(`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		// The upstream's own check, on the chat form it builds: every result
		// of a run must follow the calls without an item between them.
		open := 0
		var sent struct {
			Input []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
			} `json:"input"`
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatal(err)
		}
		for _, it := range sent.Input {
			switch it.Type {
			case "function_call":
				open++
			case "function_call_output":
				open--
			default:
				if open > 0 {
					return 400, `{"error":{"message":"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'."}}`
				}
			}
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	body := `{"model":"fake/m1","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"look"}]},` +
		`{"type":"function_call","call_id":"call_a","name":"view_image","arguments":"{}"},` +
		`{"type":"function_call","call_id":"call_b","name":"exec_command","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_a","output":"looked"},` +
		`{"type":"message","role":"developer","content":[{"type":"input_text","text":` + jsonStr(resizeNotice) + `}]},` +
		`{"type":"function_call_output","call_id":"call_b","output":"ran"}]}`
	if code, out := post(t, "/v1/responses", body); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	want := []string{"message:", "function_call:call_a", "function_call:call_b",
		"function_call_output:call_a", "function_call_output:call_b", "message:"}
	order := orderOf(t, f.got)
	if len(order) != len(want) {
		t.Fatalf("sent %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("sent %v, want %v", order, want)
		}
	}
}
