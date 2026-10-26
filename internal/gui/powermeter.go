package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/yetone/magpie/internal/powermeterproc"
	"github.com/yetone/magpie/internal/powermeterproxy"
	"github.com/yetone/magpie/internal/settings"
)

// The power-meter sidecar: one supervisor per process, built the first time
// the setting is on and stopped when the user turns it off again.
//
// The setting is read on every request rather than once at start-up, because
// the Balance page turns it on where the user already is: a window open when
// the switch is flipped has to reach a sidecar that starts then, and a
// window whose sidecar was turned off must stop reaching one.
var (
	powerMeterMu  sync.Mutex
	powerMeterSup *powermeterproc.Supervisor
)

// powerMeterSupervisor returns the supervisor the setting asks for, building
// and starting it the first time it is on and stopping the one it built when
// the setting goes off. nil means off.
func powerMeterSupervisor() *powermeterproc.Supervisor {
	powerMeterMu.Lock()
	if !settings.Load().PowerMeter {
		old := powerMeterSup
		powerMeterSup = nil
		powerMeterMu.Unlock()
		if old != nil {
			// outside the lock: Stop waits for the child to go (up to a
			// couple of seconds), and a status read must not wait on it
			old.Stop()
		}
		return nil
	}
	if powerMeterSup == nil {
		sup := powermeterproc.New(powermeterproc.Options{Enabled: true})
		sup.Start()
		powerMeterSup = sup
	}
	sup := powerMeterSup
	powerMeterMu.Unlock()
	return sup
}

// errPowerMeterOff answers a health probe with no supervisor behind it. The
// proxy never asks one while Enabled is false; this is the honest answer if
// it ever does.
var errPowerMeterOff = errors.New("power-meter sidecar is off")

// liveSupervisor answers the proxy from the setting as it is now. The proxy
// is built once per window, so a supervisor captured then would outlive the
// switch that stopped it; this asks each time instead.
type liveSupervisor struct{}

func (liveSupervisor) Enabled() bool {
	s := powerMeterSupervisor()
	return s != nil && s.Enabled()
}

func (liveSupervisor) Running() bool {
	s := powerMeterSupervisor()
	return s != nil && s.Running()
}

func (liveSupervisor) URL() string {
	if s := powerMeterSupervisor(); s != nil {
		return s.URL()
	}
	return ""
}

func (liveSupervisor) Token() string {
	if s := powerMeterSupervisor(); s != nil {
		return s.Token()
	}
	return ""
}

func (liveSupervisor) Health(ctx context.Context) error {
	if s := powerMeterSupervisor(); s != nil {
		return s.Health(ctx)
	}
	return errPowerMeterOff
}

func (liveSupervisor) LastError() string {
	if s := powerMeterSupervisor(); s != nil {
		return s.LastError()
	}
	return ""
}

// powerMeterRoutes mounts /api/powermeter/ on mux and the switch the Balance
// page uses to turn its sidecar on and off. Called from Handler for every
// window.
func powerMeterRoutes(mux *http.ServeMux) {
	powermeterproxy.New(liveSupervisor{}).Register(mux)
	// the Balance page's own switch: the sidecar is opt-in, and this is
	// where the page offering it can turn it on
	mux.HandleFunc("POST /api/settings/power-meter", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		if err := settings.SetPowerMeter(in.On); err != nil {
			fail(rw, err)
			return
		}
		// the supervisor follows the setting on the next read (the page's
		// status poll); stopping one that is going takes a moment, so it is
		// not waited for here
		writeJSON(rw, settingsState())
	})
}
