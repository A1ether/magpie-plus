// Package powermeter is the Balance page: power-meter's multi-provider
// balance and expiry board inside magpie's window. The page reads the
// sidecar through /api/powermeter/ (internal/powermeterproxy); whether the
// sidecar runs is settings.PowerMeter, a separate switch from this plugin's
// (Plugins › Built-in). Off, the page leaves the window.
package powermeter

import "github.com/yetone/magpie/internal/pluginrt"

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "powermeter", API: "^1", Requires: []string{"window"}, Essential: true,
		Name: "Balance board", Sub: "Every provider's balance and each credit lot's expiry, from the power-meter sidecar",
		Parts:  []pluginrt.Part{{Slot: "nav.page", Key: "powermeter"}},
		Client: []string{"i18n.js", "balance.css", "client.js", "accounts.js", "page.js"},
	})
}
