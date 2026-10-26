package powermeterproc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// disabledStaysDown: an opt-out supervisor never runs a child and reports
// not-running, so a magpie without the setting has no sidecar.
func TestDisabledStaysDown(t *testing.T) {
	s := New(Options{})
	if s.Enabled() {
		t.Fatal("zero-value supervisor must be disabled")
	}
	s.Start()
	defer s.Stop()
	if s.Running() {
		t.Fatal("disabled supervisor reported running")
	}
	if s.URL() != "" {
		t.Fatalf("disabled supervisor had a URL %q", s.URL())
	}
}

// fakeReady is a Command that prints power-meter's ready line with a fixed
// loopback URL and then sleeps, so runOnce parses the URL and stays up until
// Stop. No network, no Bun: a /bin/sh one-liner.
func fakeReady(url string) func(context.Context, Options, string) *exec.Cmd {
	return func(ctx context.Context, _ Options, token string) *exec.Cmd {
		if token == "" {
			panic("the child must get the supervisor's token")
		}
		return exec.CommandContext(ctx, "sh", "-c",
			"echo '[power-meter] listening at "+url+" mode=sidecar'; sleep 30")
	}
}

func TestStartParsesReadyURL(t *testing.T) {
	s := New(Options{
		Enabled: true,
		Command: fakeReady("http://127.0.0.1:59999"),
	})
	s.Start()
	defer s.Stop()

	// Wait on state (the URL), not a sleep: poll until the supervisor has it
	// or the test's own deadline passes.
	deadline := time.Now().Add(5 * time.Second)
	for s.URL() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.URL(); got != "http://127.0.0.1:59999" {
		t.Fatalf("URL = %q, want the ready line's URL", got)
	}
	if !s.Running() {
		t.Fatal("child printed ready but supervisor isn't running")
	}
	if s.Token() == "" {
		t.Fatal("a supervisor always mints a token")
	}
}

// TestStopEndsChild: Stop must end the child it started (the code standard
// "whatever a test starts ends when it does"). The fake child writes a pid
// file; after Stop the process must be gone.
func TestStopEndsChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	s := New(Options{
		Enabled: true,
		Command: func(ctx context.Context, _ Options, _ string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c",
				"echo '[power-meter] listening at http://127.0.0.1:59998 mode=sidecar'; echo $$ > "+pidFile+"; exec sleep 30")
		},
	})
	s.Start()
	deadline := time.Now().Add(5 * time.Second)
	for s.URL() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.URL() == "" {
		t.Fatal("child never came up")
	}
	s.Stop()
	if s.Running() {
		t.Fatal("still running after Stop")
	}
	// The pid file holds the sleep that took over from sh (exec). It must be
	// dead now, or the tree wasn't swept.
	if b, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(b))
		if err := exec.Command("kill", "-0", pid).Run(); err == nil {
			t.Fatalf("child pid %s still alive after Stop", pid)
		}
	}
}

// TestCrashBackoff: a child that exits at once is restarted, and consecutive
// early crashes grow the backoff rather than hot-looping.
func TestCrashBackoff(t *testing.T) {
	starts := make(chan struct{}, 32)
	s := New(Options{
		Enabled:        true,
		RestartBackoff: 20 * time.Millisecond,
		Command: func(ctx context.Context, _ Options, _ string) *exec.Cmd {
			select {
			case starts <- struct{}{}:
			default:
			}
			// Crash at once, before any ready line.
			return exec.CommandContext(ctx, "sh", "-c", "exit 1")
		},
	})
	s.Start()
	defer s.Stop()

	// Count restarts over a window well past several backoffs; the child must
	// have been restarted (more than one start) but not in a tight loop.
	time.Sleep(300 * time.Millisecond)
	n := len(starts)
	if n < 2 {
		t.Fatalf("a crashing child was restarted %d times, want at least one restart", n)
	}
	// 20ms -> 40 -> 80 -> 160 ... in 300ms that's ~4 starts; a hot loop would
	// be dozens. Leave room on both sides of the timing.
	if n > 12 {
		t.Fatalf("a crashing child hot-looped: %d starts in 300ms", n)
	}
}

// TestConfigDirIsCreated: the child runs in ConfigDir, so a clean machine's
// first start must find it there rather than fail its chdir. Nested, too: the
// parent <magpie config> may not exist either.
func TestConfigDirIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "magpie", "powermeter")
	s := New(Options{ConfigDir: dir})
	if err := s.ensureConfigDir(); err != nil {
		t.Fatalf("ensureConfigDir: %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the config dir wasn't created: %v", err)
	}
	if !st.IsDir() {
		t.Fatalf("%s isn't a directory", dir)
	}
	// A second call on an existing dir is a no-op, not an error.
	if err := s.ensureConfigDir(); err != nil {
		t.Fatalf("ensureConfigDir on an existing dir: %v", err)
	}
}

// TestLastErrorSaysWhy: a sidecar that can't start records why, so the page's
// Diagnostics can say it instead of "starting" for ever; a child that does
// come up clears it.
func TestLastErrorSaysWhy(t *testing.T) {
	s := New(Options{
		Enabled:        true,
		RestartBackoff: 20 * time.Millisecond,
		Command: func(ctx context.Context, _ Options, _ string) *exec.Cmd {
			return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "not-a-binary"))
		},
	})
	s.Start()
	defer s.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for s.LastError() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.LastError() == "" {
		t.Fatal("a child that never started left LastError empty")
	}
	if s.URL() != "" || s.Running() {
		t.Fatalf("a failed start reported up: url %q running %v", s.URL(), s.Running())
	}

	// The other half: a child that prints its ready line clears the reason.
	up := New(Options{Enabled: true, Command: fakeReady("http://127.0.0.1:59997")})
	up.Start()
	defer up.Stop()
	deadline = time.Now().Add(5 * time.Second)
	for up.URL() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if up.URL() == "" {
		t.Fatal("the healthy child never came up")
	}
	if e := up.LastError(); e != "" {
		t.Fatalf("LastError = %q with the child up, want empty", e)
	}
}

// TestTokenIsRandomPerRun: two supervisors mint different tokens, so a stale
// one from a prior run can't drive this run's sidecar.
func TestTokenIsRandomPerRun(t *testing.T) {
	a, b := New(Options{}), New(Options{})
	if a.Token() == b.Token() {
		t.Fatal("two supervisors share a token")
	}
}
