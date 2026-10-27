package gateway

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The reporter's schema (#3481): a generic JSON value whose type is a union,
// carried beside items: {} and a constraint of the object and array types.
const unionValueSchema = `{"type":"object","required":["value"],"properties":{
	"value":{"type":["null","boolean","object","array","number","string"],"items":{},"description":"any JSON value"},
	"tags":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":4}}}`

// codeAssistToolDecl builds one tool's declaration for an app and model.
func codeAssistToolDecl(t *testing.T, model, agent, inputSchema string) map[string]any {
	t.Helper()
	r, err := parseAnthropic([]byte(`{"model":"x","max_tokens":100,
		"tools":[{"name":"mcp__generic","description":"generic","input_schema":` + inputSchema + `}],
		"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Request struct {
			Tools []struct {
				FunctionDeclarations []map[string]any `json:"functionDeclarations"`
			} `json:"tools"`
		} `json:"request"`
	}
	if err := json.Unmarshal(buildCodeAssist(r, model, agent), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Request.Tools) != 1 || len(env.Request.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("%s/%s: tools %+v", agent, model, env.Request.Tools)
	}
	return env.Request.Tools[0].FunctionDeclarations[0]
}

// Antigravity's Gemini models take a tool's schema as parametersJsonSchema,
// as Gemini CLI and Vertex AI send it, so the reporter's union type and its
// constraints reach Code Assist as they are (#3481).
func TestAntigravityGeminiKeepsToolSchema(t *testing.T) {
	decl := codeAssistToolDecl(t, "gemini-3.8-flash", "antigravity", unionValueSchema)
	if _, ok := decl["parameters"]; ok {
		t.Fatalf("Gemini on Antigravity got parameters: %v", decl)
	}
	sent, ok := decl["parametersJsonSchema"]
	if !ok {
		t.Fatalf("Gemini on Antigravity got %v, want parametersJsonSchema", decl)
	}
	var got, want any
	b, _ := json.Marshal(sent)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(unionValueSchema), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parametersJsonSchema =\n%s\nwant\n%s", b, unionValueSchema)
	}

	// the reported fixture as a tool's whole schema: Gemini keeps it as sent
	const unionWithItems = `{"type":["null","boolean","object","array","number","string"],"items":{}}`
	decl = codeAssistToolDecl(t, "gemini-3.8-flash", "antigravity", unionWithItems)
	var litGot, litWant any
	b, _ = json.Marshal(decl["parametersJsonSchema"])
	if err := json.Unmarshal(b, &litGot); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(unionWithItems), &litWant); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(litGot, litWant) {
		t.Errorf("parametersJsonSchema = %s, want %s", b, unionWithItems)
	}
}

// Antigravity's Claude models keep the OpenAPI-style parameters. The union
// carrying items resolves to array under the current projection, so its
// array keywords are valid there; an object-only keyword still goes (#3481).
func TestAntigravityClaudeStillProjectsToolSchema(t *testing.T) {
	decl := codeAssistToolDecl(t, "claude-sonnet-4-6", "antigravity", unionValueSchema)
	if _, ok := decl["parametersJsonSchema"]; ok {
		t.Fatalf("Claude on Antigravity got parametersJsonSchema: %v", decl)
	}
	params, ok := decl["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("Claude on Antigravity got %v, want parameters", decl)
	}
	props, _ := params["properties"].(map[string]any)
	value, _ := props["value"].(map[string]any)
	if value["type"] != "array" || value["nullable"] != true {
		t.Errorf("union = %v", value)
	}
	if !reflect.DeepEqual(value["items"], map[string]any{}) {
		t.Errorf("union items = %v", value["items"])
	}
	for _, k := range []string{"properties", "minProperties", "maxProperties"} {
		if _, ok := value[k]; ok {
			t.Errorf("union kept object keyword %q: %v", k, value)
		}
	}
	if value["description"] != "any JSON value" {
		t.Errorf("description lost: %v", value)
	}
	tags, _ := props["tags"].(map[string]any)
	if tags["type"] != "array" || !reflect.DeepEqual(tags["items"], map[string]any{"type": "string"}) ||
		tags["minItems"] != float64(1) || tags["maxItems"] != float64(4) {
		t.Errorf("tags = %v", tags)
	}
	if !reflect.DeepEqual(params["required"], []any{"value"}) {
		t.Errorf("required = %v", params["required"])
	}
}

// Only Antigravity's Claude gets the projected parameters; its Gemini models
// and every other Code Assist and Vertex AI path carry parametersJsonSchema.
func TestCodeAssistToolSchemaFieldByAgent(t *testing.T) {
	const schema = `{"type":"object","properties":{"s":{"type":"string"}}}`
	for _, c := range []struct{ agent, model, field string }{
		{"gemini", "gemini-2.5-pro", "parametersJsonSchema"},
		{"vertex", "gemini-3-flash", "parametersJsonSchema"},
		{"antigravity", "gemini-3-flash", "parametersJsonSchema"},
		{"antigravity", "claude-sonnet-4-6", "parameters"},
	} {
		decl := codeAssistToolDecl(t, c.model, c.agent, schema)
		if _, ok := decl[c.field]; !ok {
			t.Errorf("%s/%s got %v, want %s", c.agent, c.model, decl, c.field)
		}
		other := "parameters"
		if c.field == "parameters" {
			other = "parametersJsonSchema"
		}
		if _, ok := decl[other]; ok {
			t.Errorf("%s/%s also got %s: %v", c.agent, c.model, other, decl)
		}
	}
}

// plainSchema keeps only the keywords of the type a schema resolved to:
// array-only fields go from one that isn't an array, object-only ones from
// one that isn't an object, and nested arrays keep both (#3481).
func TestPlainSchemaDropsKeywordsOfAnotherType(t *testing.T) {
	in := `{"type":"object","properties":{
		"scalar":{"type":"string","items":{"type":"string"},"minItems":1,"maxItems":2,"pattern":"^a","description":"a"},
		"map":{"type":"object","properties":{"a":{"type":"string"}},"minProperties":1,"maxProperties":2,"items":{"type":"string"},"minItems":3,"maxItems":4},
		"list":{"type":"array","items":{"type":"array","items":{"type":"string"}},"minItems":1,"maxItems":2,"properties":{"x":{"type":"string"}},"minProperties":3,"maxProperties":4},
		"union":{"type":["null","boolean","object","array","number","string"],"items":{},"properties":{"x":{"type":"string"}},"minItems":1,"maxItems":2,"minProperties":3,"maxProperties":4,"description":"any"},
		"any":{"anyOf":[{"type":"null"},{"type":"boolean","items":{},"minItems":1}]},
		"grid":{"type":"array","items":{"type":"array","items":{"type":"object","properties":{"cell":{"type":"string"}}}}}
	}}`
	want := `{"type":"object","properties":{
		"scalar":{"type":"string","pattern":"^a","description":"a"},
		"map":{"type":"object","properties":{"a":{"type":"string"}},"minProperties":1,"maxProperties":2},
		"list":{"type":"array","items":{"type":"array","items":{"type":"string"}},"minItems":1,"maxItems":2},
		"union":{"type":"object","properties":{"x":{"type":"string"}},"minProperties":3,"maxProperties":4,"nullable":true,"description":"any"},
		"any":{"type":"boolean","nullable":true},
		"grid":{"type":"array","items":{"type":"array","items":{"type":"object","properties":{"cell":{"type":"string"}}}}}
	}}`
	var got, exp any
	if err := json.Unmarshal(plainSchema(json.RawMessage(in)), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, exp) {
		b, _ := json.Marshal(got)
		t.Errorf("got %s\nwant %s", b, want)
	}
}

// The reported fixture on its own: a union type with items: {} resolves to
// array under the current projection, where items is valid (#3481).
func TestPlainSchemaUnionWithItemsIsArray(t *testing.T) {
	in := `{"type":["null","boolean","object","array","number","string"],"items":{}}`
	var got any
	if err := json.Unmarshal(plainSchema(json.RawMessage(in)), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"type": "array", "items": map[string]any{}, "nullable": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
