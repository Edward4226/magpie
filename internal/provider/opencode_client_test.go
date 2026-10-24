package provider

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// openCodeDecode reads an id as OpenCode's identifier.ts lays it out: the
// time in milliseconds (kept to 36 bits) and the count of ids made in it.
func openCodeDecode(t *testing.T, id string, descending bool) (ms, counter uint64) {
	t.Helper()
	v, err := strconv.ParseUint(id[4:16], 16, 64)
	if err != nil {
		t.Fatalf("%q: %v", id, err)
	}
	if descending {
		v = ^v & 0xffffffffffff
	}
	return v / 0x1000, v % 0x1000
}

// Zen's free tier answers 403 FreeTierError ("can only be used from within
// OpenCode") to a session id OpenCode couldn't have made (c9cu on Discord:
// muse-spark-1.3-contributor-free from Pi worked one day and not the
// next). Checked against opencode.ai with the same request:
// ses_e9283da055b15JwOAra76bwUx4, a sha256 cut to the shape that magpie
// sent, got 403; ses_ecc5d8f28ffe9Cy14V8Em7lI2u, from a real OpenCode
// 1.19.0 run, got 200. The session made is OpenCode's: descending, the
// time it was made, a small count.
func TestOpenCodeSessionIsOpenCodes(t *testing.T) {
	before := uint64(time.Now().UnixMilli()) % (1 << 36)
	id := openCodeSession("conversation-a")
	after := uint64(time.Now().UnixMilli()) % (1 << 36)
	if !openCodeIDRe.MatchString(id) || id[:4] != "ses_" || !openCodeRealSession(id) {
		t.Fatalf("session %q", id)
	}
	if ms, n := openCodeDecode(t, id, true); ms < before || ms > after || n < 1 || n >= 16 {
		t.Fatalf("session %q: time %d (want %d..%d), count %d", id, ms, before, after, n)
	}
	// one conversation keeps one session, another gets its own
	if again, other := openCodeSession("conversation-a"), openCodeSession("conversation-b"); again != id || other == id {
		t.Fatalf("sessions %q %q %q", id, again, other)
	}
	if a, b := openCodeSession(""), openCodeSession(""); a == b || !openCodeRealSession(a) {
		t.Fatalf("no conversation: %q %q", a, b)
	}
	// OpenCode's own goes as it is; a hash cut to its shape doesn't
	const real, hashed = "ses_ecc5d8f28ffe9Cy14V8Em7lI2u", "ses_e9283da055b15JwOAra76bwUx4"
	if got := openCodeSession(real); got != real {
		t.Fatalf("OpenCode's own session became %q", got)
	}
	if openCodeRealSession(hashed) {
		t.Fatal("a hash passes for OpenCode's session")
	}
	if got := openCodeSession(hashed); got == hashed || !openCodeRealSession(got) {
		t.Fatalf("a hashed session went on as %q", got)
	}
	h := http.Header{"X-Session-Affinity": {"from-the-agent"}}
	OpenCodeClient(h, "conversation-a")
	if h.Get("x-opencode-session") != id || h.Get("x-opencode-session-id") != id || h.Get("User-Agent") != OpenCodeUserAgent {
		t.Fatalf("headers %v", h)
	}
	// Zen's free tier refuses a request carrying x-session-affinity, which
	// OpenCode itself doesn't send it
	if h.Get("x-session-affinity") != "" || h.Get("X-Session-Id") != "" {
		t.Fatalf("headers OpenCode doesn't send: %v", h)
	}
	if req := h.Get("x-opencode-request"); req[:4] != "msg_" || !openCodeIDRe.MatchString(req) {
		t.Fatalf("request %q", req)
	}
}

// Ids made in one millisecond count up, as OpenCode's do; a request's is
// ascending.
func TestOpenCodeIDsCountUp(t *testing.T) {
	now := time.UnixMilli(1_790_000_000_000)
	first, second := openCodeNewID("msg", false, now), openCodeNewID("msg", false, now)
	if ms, n := openCodeDecode(t, first, false); ms != 1_790_000_000_000%(1<<36) || n != 1 {
		t.Fatalf("%q: time %d count %d", first, ms, n)
	}
	if _, n := openCodeDecode(t, second, false); n != 2 || second <= first {
		t.Fatalf("%q after %q: count %d", second, first, n)
	}
}

// The memory cache is bounded, but a conversation moved out of it keeps the
// session OpenCode already routed and cached.
func TestOpenCodeSessionsSurviveEviction(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const seed = "bounded-0"
	first := openCodeSession(seed)
	for i := 1; i <= openCodeKeep; i++ {
		openCodeSession("bounded-" + strconv.Itoa(i))
	}
	openCodeIDs.Lock()
	n := openCodeIDs.order.Len()
	openCodeIDs.Unlock()
	if n > openCodeKeep {
		t.Fatalf("%d kept", n)
	}
	if again := openCodeSession(seed); again != first {
		t.Fatalf("the oldest conversation's session changed from %q to %q", first, again)
	}
}

// A magpie restart is another process: a conversation OpenCode already
// routed and cached keeps the same session after it.
func TestOpenCodeSessionSurvivesRestart(t *testing.T) {
	if seed := os.Getenv("MAGPIE_OPENCODE_SESSION_SEED"); seed != "" {
		h := http.Header{}
		OpenCodeClient(h, seed)
		fmt.Printf("OPENCODE_SESSION:%s:%s\n", h.Get("x-opencode-session"), h.Get("x-opencode-session-id"))
		return
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	const seed = "11111111-2222-4333-8444-555555555555"
	first := openCodeSessionInAnotherProcess(t, seed)
	second := openCodeSessionInAnotherProcess(t, seed)
	if first != second {
		t.Fatalf("one conversation across a restart: %q, then %q", first, second)
	}
	b, err := os.ReadFile(filepath.Join(cfg, "magpie", "opencode-sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), seed) {
		t.Fatalf("the session file holds the conversation seed:\n%s", b)
	}
}

func openCodeSessionInAnotherProcess(t *testing.T, seed string) string {
	t.Helper()
	cmd := proc.Command(os.Args[0], "-test.run=^TestOpenCodeSessionSurvivesRestart$", "-test.count=1")
	cmd.Env = append(os.Environ(), "MAGPIE_OPENCODE_SESSION_SEED="+seed)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("another magpie: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if body, ok := strings.CutPrefix(line, "OPENCODE_SESSION:"); ok {
			id, id2, ok := strings.Cut(body, ":")
			if !ok || id != id2 || !openCodeRealSession(id) {
				t.Fatalf("another magpie's session headers: %q", body)
			}
			return id
		}
	}
	t.Fatalf("another magpie named no session:\n%s", out)
	return ""
}

// The first request for a conversation may arrive many times at once: all
// of them must name the one session OpenCode is given.
func TestOpenCodeSessionConcurrentFirstUse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const seed = "concurrent-conversation"
	ids := make([]string, 32)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = openCodeSession(seed)
		}(i)
	}
	wg.Wait()
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("request %d named %q, request 0 named %q", i, id, ids[0])
		}
	}
}

// A session file that doesn't parse is copied aside before a new one is
// written, and nothing from the broken file is taken as a mapping.
func TestOpenCodeSessionsBadFileIsKeptAside(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "magpie", "opencode-sessions.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const broken = `{"version":1,"sessions":`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if id := openCodeSession("bad-file-conversation"); !openCodeRealSession(id) {
		t.Fatalf("session %q", id)
	}
	backups, err := filepath.Glob(path + ".bad-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v, %v", backups, err)
	}
	if b, err := os.ReadFile(backups[0]); err != nil || string(b) != broken {
		t.Fatalf("backup: %q, %v", b, err)
	}
	if _, _, err := readOpenCodeSessions(path); err != nil {
		t.Fatalf("the new session file: %v", err)
	}
}

// A dotfiles setup may link the session file elsewhere. Writing one must
// keep the link rather than turn it into a file of its own.
func TestOpenCodeSessionsFollowSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "magpie", "opencode-sessions.json")
	target := filepath.Join(t.TempDir(), "dotfiles", "opencode-sessions.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if id := openCodeSession("symlinked-conversation"); !openCodeRealSession(id) {
		t.Fatalf("session %q", id)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the session link: %v, %v", fi, err)
	}
	if _, _, err := readOpenCodeSessions(target); err != nil {
		t.Fatalf("the linked session file: %v", err)
	}
}
