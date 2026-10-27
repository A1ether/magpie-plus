package gui

// appName is what the desktop shows this app as: the menu bar's item, the
// window's title, About/Hide/Quit in the Mac's app menu, and the .app's name
// in the Finder (build/darwin/Info.plist says it too). This fork is magpie
// plus: magpie with the Balance board and power-meter's sidecar.
const appName = "magpie plus"

// programName is the name magpie gives GTK on Linux (g_set_prgname, through
// Wails' LinuxOptions.ProgramName). GTK3 makes it the window's Wayland
// app_id and its X11 WM_CLASS ("magpie", "Magpie"); without it both are the
// executable's name, magpie-linux-amd64 when the release is run as
// downloaded, which no desktop entry names, so the dock drew a gear (#2626).
// The entry registerScheme writes names it in StartupWMClass. WebKitGTK keys
// its data folder by it too (~/.local/share/magpie), see keepWebData.
const programName = "magpie"
