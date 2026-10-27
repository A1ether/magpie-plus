package gui

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// appName is what the desktop shows this app as: the menu bar's item, the
// window's title, About/Hide/Quit in the Mac's app menu, and the .app's name
// in the Finder (build/darwin/Info.plist says it too). This fork is magpie
// plus: magpie with the Balance board and power-meter's sidecar.
const appName = "magpie plus"

// appIdentity tells this app apart from the magpie it was forked from, so
// the two can run side by side: the bundle id of the .app this program
// lives in, which is what a Mac, Login Items and notifications already
// tell the two apart by. "" when it lives in no bundle, or is Windows.
//
// It separates the two things an installed magpie otherwise shares with
// the other by living under one home folder, and that must not be shared:
//   - the instance lock (instance_unix.go): without it both apps take the
//     same lock, so the second one to start hands over and quits;
//   - Wails' single instance off the Mac (app.go's singleInstance).
//
// The config folder (appdir.Config) is deliberately NOT separated: both
// apps read one ~/.config/magpie, so an account signed in once is signed
// in for both, and the usage history is one. The price is that the two
// share a gateway port: whichever starts first serves it and the other is
// a page beside it, and a setting saved in one window while the other is
// open is the last one saved. That is what sharing the folder means.
//
// A build from source, `magpie web` and the terminal's magpie run from no
// .app and keep upstream's own lock and id as before. So does Windows,
// where the instance is Wails' to keep.
// appIdentity is read afresh each call rather than once: it is a small
// read of this program's own Info.plist, and a test has to be able to ask
// for one identity and then another. The app's own calls happen once at
// startup (the lock, the single instance), so the cost is paid once.
func appIdentity() string {
	if id, ok := os.LookupEnv("MAGPIE_IDENTITY"); ok {
		return id // "" says upstream's own, which a test can ask for
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	return bundleIdentity()
}

// bundlePlistID reads an app's bundle id out of its Info.plist.
var bundlePlistID = regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>([^<]+)</string>`)

// bundleIdentity is the CFBundleIdentifier of the .app this program runs
// from, or "" when it runs from no bundle.
func bundleIdentity() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// …/magpie plus.app/Contents/MacOS/magpie
	macos := filepath.Dir(exe)
	if filepath.Base(macos) != "MacOS" || filepath.Base(filepath.Dir(macos)) != "Contents" {
		return ""
	}
	app := filepath.Dir(filepath.Dir(macos))
	if !strings.EqualFold(filepath.Ext(app), ".app") {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	m := bundlePlistID.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// programName is the name magpie gives GTK on Linux (g_set_prgname, through
// Wails' LinuxOptions.ProgramName). GTK3 makes it the window's Wayland
// app_id and its X11 WM_CLASS ("magpie", "Magpie"); without it both are the
// executable's name, magpie-linux-amd64 when the release is run as
// downloaded, which no desktop entry names, so the dock drew a gear (#2626).
// The entry registerScheme writes names it in StartupWMClass. WebKitGTK keys
// its data folder by it too (~/.local/share/magpie), see keepWebData.
const programName = "magpie"
