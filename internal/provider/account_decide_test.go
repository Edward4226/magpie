package provider

import (
	"testing"
	"time"
)

// decide= set on a signed-in provider stays: it named the plan's System
// One endpoint, the one place the plan's Jev answers, and Save's account
// whitelist dropped it — the CLI said "✓ saved" and the field went
// nowhere, so a routing group could never reach the subscription's
// decision model (#3349, Arcadia822).
func TestAccountKeepsItsDecide(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	if err := addGoogleLogin("antigravity", "u@example.com", "", googleAuth{
		AccessToken: "t", RefreshToken: "rt", Project: "p",
		Expiry: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := addGoogleLogin("antigravity", "second@example.com", "", googleAuth{
		AccessToken: "t2", RefreshToken: "rt2", Project: "p2",
		Expiry: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := switchGoogleLogin("antigravity", "u@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := setGoogleLoginOn("antigravity", "second@example.com", true); err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "antigravity", Decide: "https://api.commandcode.ai/provider/v1", Models: []string{"typesafe/jev"}}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if p.Decide != "https://api.commandcode.ai/provider/v1" {
		t.Fatalf("the account's decide: %q", p.Decide)
	}
	if p.Account == nil {
		t.Fatal("the stored decide entry stood in for the sign-in")
	}
	if !p.Decides() {
		t.Fatal("the account does not answer System One")
	}
	// every account of the provider keeps it, as it keeps the queue (#1899)
	for _, q := range p.AlsoOn() {
		if q.Decide != "https://api.commandcode.ai/provider/v1" {
			t.Fatalf("also on: decide %q", q.Decide)
		}
	}
}
