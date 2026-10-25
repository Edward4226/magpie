package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// Command Code's endpoint parts are internal fetch metadata. A part may
// still name a model its plan excludes; /api/providers lists the filtered
// merged models, never the union of the parts.
func TestCommandCodeSavedSidesStayOutOfProvidersAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	const chat = "https://api.commandcode.ai/provider/v1"
	if err := provider.Save(provider.Provider{
		ID: "commandcode", Name: "Command Code", Key: "sk-cmd", Chat: chat,
		Responses: chat, Anthropic: "https://api.commandcode.ai/provider",
	}); err != nil {
		t.Fatal(err)
	}
	allowed := []catalog.Model{
		{ID: "deepseek/deepseek-v4.1-flash"},
		{ID: "claude-sonnet-5-5"},
	}
	all := append(append([]catalog.Model{}, allowed...),
		catalog.Model{ID: "claude-sonnet-5"}, catalog.Model{ID: "gpt-6.1-sol"})
	if err := catalog.SaveLiveSides("commandcode", chat, allowed, map[string][]catalog.Model{
		chat:                                  all,
		"https://api.commandcode.ai/provider": all,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var state providersJSON
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, p := range state.Providers {
		if p.ID != "commandcode" {
			continue
		}
		got := map[string]bool{}
		for _, m := range p.Models {
			got[m.ID] = true
		}
		for _, id := range []string{"deepseek/deepseek-v4.1-flash", "claude-sonnet-5-5"} {
			if !got[id] {
				t.Errorf("/api/providers left out %s: %v", id, got)
			}
		}
		for _, id := range []string{"claude-sonnet-5", "gpt-6.1-sol"} {
			if got[id] {
				t.Errorf("/api/providers brought back plan-excluded %s: %v", id, got)
			}
		}
		return
	}
	t.Fatal("commandcode not listed")
}
