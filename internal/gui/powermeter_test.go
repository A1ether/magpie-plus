package gui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agentenv"
	"github.com/yetone/magpie/internal/settings"
)

// isolateSettings points settings at a fresh folder, so a test writes its own
// settings.json and never the user's.
func isolateSettings(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, "AppData", "Local"))
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
}

// TestPowerMeterSwitch: the Balance page's own switch turns the sidecar on
// and off, and the answer is the settings the page reads back. Off it is
// opt-in: nothing is on until the user presses it.
func TestPowerMeterSwitch(t *testing.T) {
	isolateSettings(t)
	if settings.Load().PowerMeter {
		t.Fatal("power-meter started on: it is opt-in")
	}
	h := Handler(nil, nil)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings/power-meter", strings.NewReader(body)))
		return rec
	}

	rec := post(`{"on":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("turn on answered %d: %s", rec.Code, rec.Body.String())
	}
	if !settings.Load().PowerMeter {
		t.Error("the switch said on, the settings say off")
	}
	if !strings.Contains(rec.Body.String(), `"powerMeter":true`) {
		t.Errorf("the answer doesn't carry the switch: %s", rec.Body.String())
	}

	if rec := post(`{"on":false}`); rec.Code != http.StatusOK {
		t.Fatalf("turn off answered %d: %s", rec.Code, rec.Body.String())
	}
	if settings.Load().PowerMeter {
		t.Error("the switch said off, the settings say on")
	}
}

// TestPowerMeterSwitchKeepsOtherSettings: the switch saves one field, never
// the whole Settings page's worth (which the page never sends).
func TestPowerMeterSwitchKeepsOtherSettings(t *testing.T) {
	isolateSettings(t)
	s := settings.Load()
	s.Lang, s.Theme, s.Tray = "zh", "dark", "window"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(nil, nil).ServeHTTP(rec,
		httptest.NewRequest("POST", "/api/settings/power-meter", strings.NewReader(`{"on":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body.String())
	}
	after := settings.Load()
	if after.Lang != "zh" || after.Theme != "dark" || after.Tray != "window" || !after.PowerMeter {
		t.Errorf("the switch lost what it doesn't own: %+v", after)
	}
}

// TestPowerMeterStatusFollowsTheSwitch: the proxy's status is the setting's
// now, not a supervisor's from start-up. Off, it reads disabled even with no
// child; on, it stops calling the sidecar disabled (starting or up, but never
// disabled), so a window open across the flip sees the change.
func TestPowerMeterStatusFollowsTheSwitch(t *testing.T) {
	isolateSettings(t)
	h := Handler(nil, nil)
	state := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/powermeter/status", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status answered %d: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		switch {
		case strings.Contains(body, `"state":"disabled"`):
			return "disabled"
		case strings.Contains(body, `"state":"up"`):
			return "up"
		case strings.Contains(body, `"state":"down"`):
			return "down"
		case strings.Contains(body, `"state":"starting"`):
			return "starting"
		}
		t.Fatalf("status said none of the four: %s", body)
		return ""
	}

	if got := state(); got != "disabled" {
		t.Fatalf("with the setting off, status is %q, want disabled", got)
	}
	if err := settings.SetPowerMeter(true); err != nil {
		t.Fatal(err)
	}
	// The sidecar has no bundle in this test's folder, so it reports why it
	// can't start rather than a state from start-up: what matters here is
	// that the setting reached the proxy.
	if got := state(); got == "disabled" {
		t.Fatal("with the setting on, status is still disabled: the switch didn't reach the proxy")
	}
}
