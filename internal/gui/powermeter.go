package gui

import (
	"net/http"
	"sync"

	"github.com/yetone/magpie/internal/powermeterproc"
	"github.com/yetone/magpie/internal/powermeterproxy"
	"github.com/yetone/magpie/internal/settings"
)

// The power-meter sidecar: one supervisor per process, started the first time
// a GUI window's mux is built with the setting on. The proxy is registered on
// every mux regardless, so a window opened with the feature off reads a clear
// "disabled" status instead of a hole.
var (
	powerMeterOnce sync.Once
	powerMeterSup  *powermeterproc.Supervisor
)

// powerMeterSupervisor returns the process-wide supervisor, building and
// starting it on first use when the user has turned the feature on. It returns
// nil when the setting is off, so the proxy reports disabled.
func powerMeterSupervisor() *powermeterproc.Supervisor {
	powerMeterOnce.Do(func() {
		if !settings.Load().PowerMeter {
			return
		}
		sup := powermeterproc.New(powermeterproc.Options{Enabled: true})
		sup.Start()
		powerMeterSup = sup
	})
	return powerMeterSup
}

// powerMeterRoutes mounts /api/powermeter/ on mux and starts the sidecar when
// the setting is on. Called from Handler for every window.
func powerMeterRoutes(mux *http.ServeMux) {
	// Pass the supervisor as the proxy's interface: a typed-nil
	// *powermeterproc.Supervisor would read as non-nil to the proxy, so the
	// disabled case hands a plain nil instead.
	var sup powermeterproxy.Supervisor
	if s := powerMeterSupervisor(); s != nil {
		sup = s
	}
	powermeterproxy.New(sup).Register(mux)
}
