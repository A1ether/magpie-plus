//go:build !windows

package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// magpie plus and the magpie it was forked from are two apps on one config
// folder, and each has to run: the lock an app takes is its own by bundle
// id (appIdentity), or the second one to start would hand over and quit.
func TestForkedAppHasItsOwnLock(t *testing.T) {
	dir := t.TempDir()
	exe := "/Applications/Magpie.app/Contents/MacOS/magpie"
	upstream := instanceLock(dir, exe)

	t.Setenv("MAGPIE_IDENTITY", "com.a1ether.magpie-plus")
	fork := instanceLock(dir, exe)
	if fork == upstream {
		t.Fatal("magpie plus shares the lock of the magpie it was forked from")
	}

	// a copy of the fork anywhere else in an .app is the fork's, and the
	// upstream app's is still upstream's: it is the app, not the path
	other := instanceLock(dir, "/Users/someone/Downloads/Magpie.app/Contents/MacOS/magpie")
	if other != fork {
		t.Error("another copy of magpie plus has a lock of its own")
	}
	t.Setenv("MAGPIE_IDENTITY", "")
	if instanceLock(dir, exe) != upstream {
		t.Error("the upstream magpie's lock moved when magpie plus appeared")
	}

	// a build from source, which has no .app over it, keeps upstream's
	if dev := instanceLock(dir, "/src/magpie/magpie-dev"); dev == upstream {
		t.Error("magpie-dev shares the app's lock")
	}

	// both locks stay in the one config folder they share
	for _, p := range []string{upstream, fork} {
		if filepath.Dir(p) != dir {
			t.Fatalf("lock %s not in the config dir", p)
		}
	}
}

// A second magpie on the same config finds the first one's lock held, a
// copy elsewhere (magpie-dev) has its own, and a lock whose holder ended,
// as when the Mac shuts down with magpie running, is free again (Crispin
// on Discord: two magpies after each boot).
func TestOneMagpieAtATime(t *testing.T) {
	dir := t.TempDir()
	if os.Getenv("MAGPIE_HOLD_LOCK") != "" {
		if held, err := holdInstance(os.Getenv("MAGPIE_HOLD_LOCK")); err != nil || !held {
			os.Exit(3)
		}
		os.Stdout.WriteString("held\n")
		select {}
	}
	lock := instanceLock(dir, "/Applications/Magpie.app/Contents/MacOS/magpie")
	if dev := instanceLock(dir, "/src/magpie/magpie-dev"); dev == lock {
		t.Fatal("magpie-dev shares the app's lock")
	}
	// every installed copy shares the lock: another magpie.app (one the
	// Mac reopens, one run translocated from Downloads) and Homebrew's of
	// any version (inaction on Discord: two birds after a restart)
	for _, other := range []string{
		"/Users/u/Downloads/magpie.app/Contents/MacOS/magpie",
		"/private/var/folders/x/T/AppTranslocation/1F2E/d/magpie.app/Contents/MacOS/magpie",
		"/opt/homebrew/Cellar/magpie/0.1.900/bin/magpie",
	} {
		if instanceLock(dir, other) != lock {
			t.Errorf("%s has a lock of its own", other)
		}
	}
	if filepath.Dir(lock) != dir {
		t.Fatalf("lock %s not in the config dir", lock)
	}

	// the first magpie: another process, killed as a shutdown would
	cmd := exec.Command(os.Args[0], "-test.run=^TestOneMagpieAtATime$")
	cmd.Env = append(os.Environ(), "MAGPIE_HOLD_LOCK="+lock)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := out.Read(buf); err != nil || string(buf[:4]) != "held" {
		cmd.Process.Kill()
		t.Fatalf("the first magpie didn't take the lock: %q %v", buf, err)
	}
	if held, err := holdInstance(lock); err != nil || held {
		cmd.Process.Kill()
		t.Fatalf("a second magpie took the held lock: %v %v", held, err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if held, err := holdInstance(lock); err != nil || !held {
		t.Fatalf("the lock of a magpie that was killed stayed held: %v %v", held, err)
	}
	heldInstance.Close()
	heldInstance = nil
}
