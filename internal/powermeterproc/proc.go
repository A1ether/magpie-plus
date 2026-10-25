// Package powermeterproc runs power-meter as magpie's sidecar: it starts the
// power-meter server, watches it, restarts it when it dies, and stops it when
// magpie stops. power-meter is the multi-provider balance/expiry board whose
// API the GUI's Balance page renders; running it as a sidecar lets magpie
// reuse power-meter's provider adapters without porting them to Go.
//
// Lifecycle: New(...) -> Start() -> URL() / Token() / Running() -> Stop().
// The supervisor goroutine starts no child until Start, and Start is a no-op
// unless Enabled. Tests never reach the network or spawn a real child unless
// they install a fake command (see proc_test.go); a child a test starts is
// always Stop()ed before the test returns.
package powermeterproc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/proc"
)

// Options configure a Supervisor. The zero value is valid but not useful:
// Enabled false means Start returns at once and the supervisor never runs.
type Options struct {
	// Enabled turns the sidecar on. It comes from a setting the user set,
	// never from a default: the sidecar is opt-in.
	Enabled bool
	// ConfigDir is where power-meter keeps accounts.json and .env. Empty
	// defaults to <magpie config>/powermeter.
	ConfigDir string
	// ServerJS is power-meter's compiled entrypoint. Empty resolves it from
	// the sidecar install (serverJSPath).
	ServerJS string
	// Host is the loopback address power-meter listens on. Empty is
	// 127.0.0.1. The sidecar never binds a non-loopback host.
	Host string
	// Command builds the child process. Nil uses the default (Bun running
	// ServerJS). Tests install a fake here so no real child or network runs.
	Command func(ctx context.Context, o Options, token string) *exec.Cmd
	// RestartBackoff floors the delay before a dead child is restarted; each
	// consecutive failure doubles it up to maxBackoff. 0 uses baseBackoff.
	RestartBackoff time.Duration
}

const (
	// baseBackoff and maxBackoff bound the restart delay growth. A child that
	// keeps dying is restarted at most every maxBackoff, not in a hot loop.
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 30 * time.Second
	// readyTimeout is how long Start waits for the child's ready line before
	// reporting it failed to come up. The URL is parsed from that line, so
	// Start waits on state (the line), not on a fixed sleep.
	readyTimeout = 15 * time.Second
)

// readyLineRe matches the line power-meter prints once it is listening:
//
//	[power-meter] listening at http://127.0.0.1:57410 mode=sidecar ...
//
// The URL is the one source of the random port: --port 0 lets the kernel
// pick it, and power-meter reads the real port back from its listener.
var readyLineRe = regexp.MustCompile(`listening at (http://[^\s]+)`)

// A Supervisor owns the one power-meter child and the goroutine that keeps
// it alive. The zero value is ready to use.
type Supervisor struct {
	opts Options

	mu      sync.Mutex
	token   string // random per run, handed to the child and the proxy
	url     string // set once the child prints its ready line
	cmd     *exec.Cmd
	tree    *proc.Tree
	running bool
	started bool
	stopped chan struct{} // closed when the supervisor loop returns
	stopReq chan struct{} // closed by Stop to ask the loop to end
}

// New returns a Supervisor for opts. ConfigDir and Host get their defaults;
// a random per-run token is minted. The token is in-memory only: it guards
// the loopback API between magpie and its own child, never written to disk.
func New(o Options) *Supervisor {
	if o.ConfigDir == "" {
		o.ConfigDir = filepath.Join(appdir.Config(), "powermeter")
	}
	if o.ServerJS == "" {
		// a power-meter build of the user's own (MAGPIE_POWERMETER_SERVER_JS)
		// overrides the installed bundle, never the other way round
		o.ServerJS = os.Getenv("MAGPIE_POWERMETER_SERVER_JS")
	}
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.RestartBackoff <= 0 {
		o.RestartBackoff = baseBackoff
	}
	s := &Supervisor{
		opts:    o,
		token:   mintToken(),
		stopReq: make(chan struct{}),
	}
	return s
}

// mintToken returns 16 random bytes hex-encoded. A fresh token each run means
// a stale one from an earlier magpie run can't drive this run's sidecar.
func mintToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not a reason to run without a token.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Token is the shared secret the proxy sends as X-Power-Meter-Token. It is
// known only to this process and its child.
func (s *Supervisor) Token() string { return s.token }

// URL is the sidecar's base address once it has come up, "" before that.
func (s *Supervisor) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

// Running reports whether the child is up right now.
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Enabled reports whether the sidecar is configured on.
func (s *Supervisor) Enabled() bool { return s.opts.Enabled }

// Start launches the supervisor loop in the background. It is a no-op when
// the sidecar is disabled or already started. The loop returns, and stopped
// is closed, when Stop is called.
func (s *Supervisor) Start() {
	if !s.opts.Enabled {
		return
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stopped = make(chan struct{})
	s.mu.Unlock()
	go s.loop()
}

// Stop asks the supervisor to end: it stops the current child and the loop,
// then waits for the loop to return. It is safe to call more than once.
func (s *Supervisor) Stop() {
	if !s.opts.Enabled {
		return
	}
	s.mu.Lock()
	select {
	case <-s.stopReq:
	default:
		close(s.stopReq)
	}
	stopped := s.stopped
	s.mu.Unlock()
	if stopped != nil {
		<-stopped
	}
}

// loop is the supervisor: start the child, wait for it to die or for Stop,
// restart it with backoff until Stop. It never runs when disabled.
func (s *Supervisor) loop() {
	defer close(s.stopped)
	backoff := s.opts.RestartBackoff
	for {
		select {
		case <-s.stopReq:
			return
		default:
		}

		err := s.runOnce()
		s.mu.Lock()
		s.running = false
		s.url = ""
		s.mu.Unlock()

		select {
		case <-s.stopReq:
			return
		case <-time.After(backoff):
		}
		// A clean run (child stayed up a while) resets the backoff; a child
		// that dies at once keeps it growing, so a broken install isn't a
		// restart storm.
		if err == nil {
			backoff = s.opts.RestartBackoff
		} else {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// runOnce starts the child, waits for its ready line, then blocks until the
// child exits or Stop is requested. It returns nil when the child ran and
// stopped for a reason other than an early crash.
func (s *Supervisor) runOnce() error {
	ctx := context.Background()
	cmd := s.buildCommand(ctx)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("powermeter stdout: %w", err)
	}
	cmd.Stderr = io.MultiWriter(os.Stderr, prefixWriter("[powermeter] ")) //nolint:errcheck // a log sink

	tree, err := proc.StartTree(cmd)
	if err != nil {
		return fmt.Errorf("starting powermeter: %w", err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.tree = tree
	s.mu.Unlock()

	// The ready line carries the random port; read stdout on its own
	// goroutine so a chatty child can't block Wait.
	ready := make(chan string, 1)
	go scanReady(stdout, ready)

	// stoppedTree closes when the child is gone; Stop can also end the wait.
	stoppedTree := make(chan error, 1)
	go func() { stoppedTree <- tree.Wait() }()

	// Wait for the ready line before reporting the child up. A child that dies
	// before printing it (a crash, a missing server.js) must return at once so
	// the supervisor backs off, not sit out the whole ready timeout.
	select {
	case url := <-ready:
		s.mu.Lock()
		s.url = url
		s.running = true
		s.mu.Unlock()
	case err := <-stoppedTree:
		return fmt.Errorf("powermeter exited before its listening line: %w", err)
	case <-time.After(readyTimeout):
		tree.Stop(2 * time.Second)
		<-stoppedTree
		return errors.New("powermeter did not print its listening line in time")
	case <-s.stopReq:
		tree.Stop(2 * time.Second)
		<-stoppedTree
		return nil
	}

	// The child is up; wait for it to die or for Stop.
	select {
	case err := <-stoppedTree:
		return err
	case <-s.stopReq:
		tree.Stop(2 * time.Second)
		<-stoppedTree
		return nil
	}
}

// scanReady reads the child's stdout line by line and sends the first URL it
// sees on the ready line. Lines without it are mirrored to magpie's log.
func scanReady(r io.Reader, ready chan<- string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if m := readyLineRe.FindStringSubmatch(line); m != nil {
			select {
			case ready <- m[1]:
			default:
			}
		}
	}
}

// buildCommand builds the child command: the installed fake in tests, else
// Bun running power-meter's server.js with the sidecar flags.
func (s *Supervisor) buildCommand(ctx context.Context) *exec.Cmd {
	if s.opts.Command != nil {
		return s.opts.Command(ctx, s.opts, s.token)
	}
	bun, err := plugin.Bun(ctx)
	if err != nil {
		// No Bun to run power-meter: report a command that fails fast, so the
		// supervisor records the failure and backs off instead of panicking.
		return proc.CommandContext(ctx, "false")
	}
	args := []string{
		s.serverJS(),
		"--port", "0",
		"--host", s.opts.Host,
		"--no-static",
		"--token", s.token,
		"--config-dir", s.opts.ConfigDir,
	}
	cmd := proc.CommandContext(ctx, bun, args...)
	cmd.Dir = s.opts.ConfigDir
	return cmd
}

// serverJS resolves power-meter's compiled entrypoint: the override when set,
// else the sidecar install under magpie's config dir.
func (s *Supervisor) serverJS() string {
	if s.opts.ServerJS != "" {
		return s.opts.ServerJS
	}
	return serverJSPath()
}

// serverJSPath is where the power-meter sidecar bundle is installed.
func serverJSPath() string {
	return filepath.Join(appdir.Config(), "powermeter", "server.js")
}

// Health is a cheap liveness probe the proxy uses for its status endpoint:
// it GETs the sidecar's /api/health (no token needed) with a short timeout.
// A failed read means unknown: it reports down, never a guessed-up.
func (s *Supervisor) Health(ctx context.Context) error {
	url := s.URL()
	if url == "" {
		return errors.New("powermeter not up yet")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("powermeter health: %s", resp.Status)
	}
	return nil
}

// prefixWriter prepends a tag to each line a child writes to stderr, so the
// sidecar's errors are told apart from magpie's own in the log.
type prefixWriter string

func (p prefixWriter) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", string(p), line)
	}
	return len(b), nil
}
