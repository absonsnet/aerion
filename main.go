package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/absonsnet/daak/app"
	"github.com/absonsnet/daak/internal/appstate"
	"github.com/absonsnet/daak/internal/logging"
	"github.com/absonsnet/daak/internal/platform"
	"github.com/absonsnet/daak/internal/settings"
	"github.com/rs/zerolog"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// Tray icon bytes: the app icon as .ico for the Windows tray and as PNG
// for macOS/Linux (the tray library wants .ico on Windows, PNG elsewhere).
// Passed to the App so the tray icon is the Daak envelope.
//
//go:embed build/windows/icon.ico
var trayIconICO []byte

//go:embed build/appicon.png
var trayIconPNG []byte

// Command-line flags
var (
	debugMode   = flag.Bool("debug", false, "Enable debug logging (same as --log-level debug)")
	logLevel    = flag.String("log-level", "", "Log level: fatal, error, warn, info, debug (default fatal; env DAAK_LOG_LEVEL)")
	logFile     = flag.String("log-file", "", "Also write logs to this file, 10 MB rollover to <file>.1 (env DAAK_LOG_FILE)")
	composeMode = flag.Bool("compose", false, "Run in composer mode (detached window)")
	viewMessage = flag.Bool("view-message", false, "Run in message viewer mode (detached popup window)")
	accountID   = flag.String("account", "", "Account ID for composer")
	folderID    = flag.String("folder-id", "", "Folder ID for message viewer popup")
	threadID    = flag.String("thread-id", "", "Thread ID for message viewer popup")
	ipcAddress  = flag.String("ipc-address", "", "IPC server address to connect to")
	mode        = flag.String("mode", "new", "Compose mode: new, reply, reply-all, forward")
	messageID   = flag.String("message-id", "", "Original message ID for reply/forward")
	draftID     = flag.String("draft-id", "", "Draft ID to resume editing")
	mailtoFlag  = flag.String("mailto", "", "Mailto URL to open in composer (detached mode)")
	dbusNotify  = flag.Bool("dbus-notify", false, "Use direct D-Bus notifications instead of portal (Linux only)")
	versionFlag = flag.Bool("version", false, "Show version and exit")
)

// DebugMode returns whether debug logging is enabled
// Can be enabled via --debug flag or DAAK_DEBUG=1 environment variable
func DebugMode() bool {
	return *debugMode || os.Getenv("DAAK_DEBUG") == "1"
}

// resolveLogConfig turns the logging flags/env vars into a logging.Config.
// Precedence: --debug / DAAK_DEBUG=1 force debug; otherwise --log-level,
// then DAAK_LOG_LEVEL, then fatal. File: --log-file, then DAAK_LOG_FILE.
// An unknown level name is a usage error: fail fast rather than silently
// logging at the wrong level for a week-long capture.
func resolveLogConfig() logging.Config {
	level := *logLevel
	if level == "" {
		level = os.Getenv("DAAK_LOG_LEVEL")
	}
	if level == "" {
		level = "fatal"
	}
	if DebugMode() {
		level = "debug"
	}
	if _, err := zerolog.ParseLevel(level); err != nil {
		fmt.Fprintf(os.Stderr, "daak: invalid --log-level %q (want fatal, error, warn, info or debug)\n", level)
		os.Exit(2)
	}

	file := *logFile
	if file == "" {
		file = os.Getenv("DAAK_LOG_FILE")
	}
	return logging.Config{Level: level, Console: true, File: file}
}

func main() {
	platform.MonitorGBMErrors()
	flag.Parse()

	if *versionFlag {
		fmt.Println(app.Version)
		return
	}

	// Logging is initialised here, once, for both the main window and
	// --compose mode (this runs before the composeMode branch).
	logCfg := resolveLogConfig()

	// On Windows, GUI apps have no console. Allocate one whenever any
	// console logging was requested (anything below fatal).
	if logCfg.Level != "fatal" {
		platform.AttachConsole()
	}
	if err := logging.Init(logCfg); err != nil {
		// A bad --log-file must not block startup; stderr logging still works.
		fmt.Fprintf(os.Stderr, "daak: logging init failed: %v\n", err)
	}

	// Check for mailto: URL in non-flag arguments
	var mailtoData *app.MailtoData
	var rawMailtoArg string
	args := flag.Args()
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), "mailto:") {
			mailtoData = app.ParseMailtoURL(arg)
			rawMailtoArg = arg
			break
		}
	}

	if *composeMode {
		runComposerMode()
		return
	}
	if *viewMessage {
		runViewerMode()
		return
	}
	runMainMode(mailtoData, rawMailtoArg)
}

// runMainMode runs the main application window
func runMainMode(mailtoData *app.MailtoData, rawMailtoArg string) {
	// Determine activation message: pass raw mailto URL if present, otherwise just "show"
	activateMsg := "show"
	if rawMailtoArg != "" {
		activateMsg = rawMailtoArg
	}

	// Single-instance detection: if another instance is running, activate it and exit
	lock := platform.NewSingleInstanceLock()
	locked, err := lock.TryLock(activateMsg)
	if err != nil {
		println("Warning: single-instance check failed:", err.Error())
	}
	if !locked {
		// Existing instance was activated
		return
	}
	defer lock.Unlock()

	// Initialize WebKit on the main thread before anything spawns GLib worker
	// threads — see webkit_preinit_linux.go (#431).
	preinitWebKit()

	// Read native title bar setting before Wails init (Frameless is init-time only)
	nativeTitleBar := false
	// Restore the last window size/maximized state (Width/Height and
	// WindowStartState are init-time options). Defaults stand on first run.
	windowWidth, windowHeight := 1280, 800
	windowStartState := options.Normal
	if paths, err := platform.GetPaths(); err == nil {
		nativeTitleBar = settings.ReadNativeTitleBar(paths.DatabasePath())
		if geo, ok := appstate.ReadWindowGeometry(paths.DatabasePath()); ok {
			if geo.Width >= 360 && geo.Width <= 10000 && geo.Height >= 400 && geo.Height <= 10000 {
				windowWidth, windowHeight = geo.Width, geo.Height
			}
			if geo.Maximized {
				windowStartState = options.Maximised
			}
		}
	}

	// Create an instance of the app structure
	application := app.NewApp(*dbusNotify)
	application.SingleInstanceLock = lock
	// Register single-instance activation before wails.Run: the lock
	// starts accepting the moment TryLock succeeds, so a second launch
	// during startup must not have its "show" dropped. The callback is
	// context-independent — commands arriving before Startup are
	// remembered and honoured once the context exists.
	if lock != nil {
		lock.SetOnShow(application.HandleSingleInstanceCommand)
	}
	application.TrayIconICO = trayIconICO
	application.TrayIconPNG = trayIconPNG

	// Store mailto data if provided (will be used after startup)
	if mailtoData != nil {
		application.PendingMailto = mailtoData
	}

	// Run pre-Wails startup checks (paths, DB open, migrations, credential
	// store). On failure, surface a native error dialog and exit before the
	// Wails window is created — otherwise the user would see a half-rendered
	// app window briefly flash before the dialog appears.
	//
	// Skipped under the `bindings` build tag — see preflight_bindings.go.
	runPreflight(application)

	// Create a dummy ComposerApp for binding generation only.
	// Wails generates JS/TS bindings at build time based on bound structs.
	// We need ComposerApp bindings for the detached composer window.
	dummyComposerApp := app.NewComposerApp(app.ComposerConfig{})

	// Create a dummy ViewerApp for binding generation only, same pattern:
	// the detached message-viewer popup binds the real ViewerApp, and its
	// frontend imports these bindings.
	dummyViewerApp := app.NewViewerApp(app.ViewerConfig{})

	// Create application with options
	err = wails.Run(&options.App{
		Title:                    platform.AppName,
		Width:                    windowWidth,
		Height:                   windowHeight,
		MinWidth:                 360,
		MinHeight:                400,
		WindowStartState:         windowStartState,
		Frameless:                !nativeTitleBar,
		StartHidden:              true, // Hide until frontend is ready to prevent white flash
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Fallback for paths the bundled assets do not have:
			// downloaded Bergamot engine and model files (Phase D).
			Handler: newBergamotFilesHandler(),
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        application.Startup,
		OnShutdown:       application.Shutdown,
		OnBeforeClose:    application.BeforeClose,
		Bind: []interface{}{
			application,
			dummyComposerApp, // For binding generation
			dummyViewerApp,   // For binding generation
		},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
			ProgramName:      "net.absons.daak",
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// runComposerMode runs a detached composer window
func runComposerMode() {
	// Validate required flags
	if *accountID == "" {
		println("Error: --account is required for composer mode")
		os.Exit(1)
	}
	if *ipcAddress == "" {
		println("Error: --ipc-address is required for composer mode")
		os.Exit(1)
	}

	// Validate compose mode
	switch *mode {
	case "new", "reply", "reply-all", "forward":
		// valid
	default:
		println("Error: --mode must be one of: new, reply, reply-all, forward")
		os.Exit(1)
	}

	// Initialize WebKit on the main thread before anything spawns GLib worker
	// threads — see webkit_preinit_linux.go (#431).
	preinitWebKit()

	// Create composer configuration
	config := app.ComposerConfig{
		AccountID:  *accountID,
		IPCAddress: *ipcAddress,
		Mode:       *mode,
		MessageID:  *messageID,
		DraftID:    *draftID,
		MailtoURL:  *mailtoFlag,
	}

	// Create composer app
	composerApp := app.NewComposerApp(config)

	// Determine window title based on mode
	title := "New Message"
	switch *mode {
	case "reply":
		title = "Reply"
	case "reply-all":
		title = "Reply All"
	case "forward":
		title = "Forward"
	}
	if *draftID != "" {
		title = "Edit Draft"
	}

	// Create a custom asset handler that serves composer.html instead of index.html
	composerAssetHandler := &composerAssetHandler{assets: assets}

	// Run Wails application for composer window.
	// The composer uses the native OS title bar (not frameless): a frameless
	// window gets no resize handles from the OS window manager on macOS, so
	// it cannot be resized or dragged by its edges. The native bar gives
	// standard resize/drag, and BeforeClose preserves the discard/save
	// confirmation when the OS close button is used.
	err := wails.Run(&options.App{
		Title:                    title,
		Width:                    1000,
		Height:                   600,
		MinWidth:                 500,
		MinHeight:                400,
		Frameless:                false,
		StartHidden:              true, // Hide until frontend is ready to prevent white flash
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{
			// Don't provide Assets here - we use Handler exclusively
			// so we can rewrite "/" to "/composer.html"
			Handler: composerAssetHandler,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        composerApp.Startup,
		OnShutdown:       composerApp.Shutdown,
		OnBeforeClose:    composerApp.BeforeClose,
		Bind: []interface{}{
			composerApp,
		},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
			ProgramName:      "net.absons.daak",
		},
	})

	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}

// runViewerMode runs a detached message-viewer popup window
func runViewerMode() {
	// Validate required flags
	if *accountID == "" {
		println("Error: --account is required for viewer mode")
		os.Exit(1)
	}
	if *folderID == "" {
		println("Error: --folder-id is required for viewer mode")
		os.Exit(1)
	}
	if *threadID == "" {
		println("Error: --thread-id is required for viewer mode")
		os.Exit(1)
	}
	if *ipcAddress == "" {
		println("Error: --ipc-address is required for viewer mode")
		os.Exit(1)
	}

	preinitWebKit()

	config := app.ViewerConfig{
		AccountID:  *accountID,
		FolderID:   *folderID,
		ThreadID:   *threadID,
		IPCAddress: *ipcAddress,
	}

	viewerApp := app.NewViewerApp(config)

	viewerAssetHandler := &viewerAssetHandler{assets: assets, models: newBergamotFilesHandler()}

	// The viewer popup uses the native OS title bar (not frameless), like
	// the detached composer: standard resize/drag, and BeforeClose keeps
	// the close confirmation path when the OS close button is used.
	err := wails.Run(&options.App{
		Title:                    platform.AppName + " - Message",
		Width:                    900,
		Height:                   650,
		MinWidth:                 500,
		MinHeight:                400,
		Frameless:                false,
		StartHidden:              true, // Hide until frontend is ready to prevent white flash
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{
			// Don't provide Assets here - we use Handler exclusively
			// so we can rewrite "/" to "/viewer.html"
			Handler: viewerAssetHandler,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        viewerApp.Startup,
		OnShutdown:       viewerApp.Shutdown,
		OnBeforeClose:    viewerApp.BeforeClose,
		Bind: []interface{}{
			viewerApp,
		},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
			ProgramName:      "net.absons.daak",
		},
	})

	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}

// bergamotFilesHandler serves downloaded Bergamot files to the
// webview (Phase D, Tier 2). The WASM engine runs in the webview, so it
// fetches its engine and models over HTTP like the spellcheck worker
// fetches its dictionaries; both live in the Daak data folder, not in
// the bundled frontend assets, so the asset server needs this fallback.
// Only paths under /bergamot/models/ (language models, from
// <data>/bergamot/models) and /bergamot/engine/ (the WASM runtime, from
// <data>/bergamot/runtime) are served, and only files inside those
// directories (path traversal is rejected). Anything else is a 404. A
// nil dataDir serves nothing.
type bergamotFilesHandler struct {
	dataDir string
}

func newBergamotFilesHandler() *bergamotFilesHandler {
	dataDir := ""
	if paths, err := platform.GetPaths(); err == nil {
		dataDir = paths.Data
	}
	return &bergamotFilesHandler{dataDir: dataDir}
}

// ServeHTTP implements http.Handler.
func (h *bergamotFilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.dataDir == "" {
		http.NotFound(w, r)
		return
	}
	var prefix, subdir string
	switch {
	case strings.HasPrefix(r.URL.Path, "/bergamot/models/"):
		prefix, subdir = "/bergamot/models/", filepath.Join("bergamot", "models")
	case strings.HasPrefix(r.URL.Path, "/bergamot/engine/"):
		prefix, subdir = "/bergamot/engine/", filepath.Join("bergamot", "runtime")
	default:
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, prefix)
	clean := strings.TrimPrefix(filepath.Clean("/"+rel), "/")
	if clean == "." || clean == "" || strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	// Resolve the file inside the base directory and refuse anything
	// that escapes (symlinks included).
	base, err := filepath.Abs(filepath.Join(h.dataDir, subdir))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil || (target != base && !strings.HasPrefix(target, base+string(os.PathSeparator))) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, target)
}

// viewerAssetHandler serves viewer.html instead of index.html for the root request.
type viewerAssetHandler struct {
	assets embed.FS
	models http.Handler
}

// ServeHTTP implements http.Handler.
// It intercepts requests for "/" and serves viewer.html instead.
func (h *viewerAssetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Bergamot model files (Phase D) are served from the data folder.
	if strings.HasPrefix(path, "/bergamot/models/") && h.models != nil {
		h.models.ServeHTTP(w, r)
		return
	}

	// Bergamot engine files (Phase D) are served from the data folder.
	if strings.HasPrefix(path, "/bergamot/engine/") && h.models != nil {
		h.models.ServeHTTP(w, r)
		return
	}

	// Rewrite root path to viewer.html
	if path == "/" || path == "" || path == "/index.html" {
		path = "/viewer.html"
	}

	// Try to read from the embedded filesystem
	subFS, err := fs.Sub(h.assets, "frontend/dist")
	if err != nil {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}

	// Create a modified request with the rewritten path
	modifiedReq := new(http.Request)
	*modifiedReq = *r
	modifiedReq.URL = new(url.URL)
	*modifiedReq.URL = *r.URL
	modifiedReq.URL.Path = path

	// Serve the file with the modified request
	http.FileServer(http.FS(subFS)).ServeHTTP(w, modifiedReq)
}

// composerAssetHandler serves composer.html instead of index.html for the root request.
type composerAssetHandler struct {
	assets embed.FS
}

// ServeHTTP implements http.Handler.
// It intercepts requests for "/" and serves composer.html instead.
func (h *composerAssetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Rewrite root path to composer.html
	if path == "/" || path == "" || path == "/index.html" {
		path = "/composer.html"
	}

	// Try to read from the embedded filesystem
	subFS, err := fs.Sub(h.assets, "frontend/dist")
	if err != nil {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}

	// Create a modified request with the rewritten path
	// This is necessary because http.FileServer uses r.URL.Path
	modifiedReq := new(http.Request)
	*modifiedReq = *r
	modifiedReq.URL = new(url.URL)
	*modifiedReq.URL = *r.URL
	modifiedReq.URL.Path = path

	// Serve the file with the modified request
	http.FileServer(http.FS(subFS)).ServeHTTP(w, modifiedReq)
}
