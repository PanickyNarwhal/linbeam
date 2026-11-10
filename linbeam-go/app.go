package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/desktop"
	"github.com/PanickyNarwhal/linbeam/pkg/installer"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

//go:embed ui/index.html
var uiAssets embed.FS

type progressEvent struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

type AppServer struct {
	cfg      *config.Config
	engine   *proton.Engine
	bluebeam *installer.BluebeamManager

	mu          sync.Mutex
	busy        bool
	subscribers map[chan progressEvent]struct{}
}

func NewAppServer(cfg *config.Config, engine *proton.Engine, bm *installer.BluebeamManager) *AppServer {
	return &AppServer{
		cfg:         cfg,
		engine:      engine,
		bluebeam:    bm,
		subscribers: make(map[chan progressEvent]struct{}),
	}
}

func (s *AppServer) broadcast(ev progressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *AppServer) StartGUI() error {
	mux := http.NewServeMux()

	uiSub, err := fs.Sub(uiAssets, "ui")
	if err != nil {
		return err
	}

	mux.Handle("/", http.FileServer(http.FS(uiSub)))
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/select-runner", s.handleSelectRunner)
	mux.HandleFunc("/api/download-proton", s.handleDownloadProton)
	mux.HandleFunc("/api/install-bluebeam", s.handleInstallBluebeam)
	mux.HandleFunc("/api/install-prereqs", s.handleInstallPrereqs)
	mux.HandleFunc("/api/repair-postinstall", s.handleRepairPostInstall)
	mux.HandleFunc("/api/launch", s.handleLaunch)
	mux.HandleFunc("/api/shortcut", s.handleShortcut)
	mux.HandleFunc("/api/winecfg", s.handleWinecfg)
	mux.HandleFunc("/api/kill", s.handleKill)
	mux.HandleFunc("/api/reset-prefix", s.handleResetPrefix)
	mux.HandleFunc("/api/events", s.handleEvents)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Printf("Linbeam UI running at %s\n", url)

	go openAppWindow(url)

	return http.Serve(listener, mux)
}

func (s *AppServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	runners, _ := s.engine.DiscoverRunners()
	activeRunner, _ := s.engine.GetActiveRunner()
	exePath, installed := s.bluebeam.FindRevuExecutable()
	if installed && (!s.cfg.RevuInstalled || s.cfg.RevuExePath != exePath) {
		s.cfg.RevuInstalled = true
		s.cfg.RevuExePath = exePath
		_ = s.cfg.Save()
	}

	payloads := s.cfg.DiscoverPayloads()

	resp := map[string]interface{}{
		"config":          s.cfg,
		"runners":         runners,
		"active_runner":   activeRunner,
		"revu_installed":  installed,
		"revu_exe":        exePath,
		"payloads":        payloads,
		"shortcut_exists": desktop.ShortcutExists(),
		"busy":            s.busy,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *AppServer) handleSelectRunner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.cfg.SelectedRunner = req.Name
	_ = s.cfg.Save()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *AppServer) handleDownloadProton(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "another operation is in progress", http.StatusConflict)
		return
	}
	s.busy = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}()

		_, err := s.engine.DownloadLatestGEProton(func(stage string, percent int) {
			s.broadcast(progressEvent{Stage: stage, Percent: percent})
		})
		if err != nil {
			s.broadcast(progressEvent{Stage: "Error: " + err.Error(), Percent: 0, Done: true, Error: err.Error()})
			return
		}
		s.broadcast(progressEvent{Stage: "Proton-GE ready!", Percent: 100, Done: true})
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func (s *AppServer) handleInstallBluebeam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RevuMSI    string `json:"revu_msi"`
		OCRMSI     string `json:"ocr_msi"`
		BPXProfile string `json:"bpx_profile"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "another operation is in progress", http.StatusConflict)
		return
	}
	s.busy = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}()

		err := s.bluebeam.InstallFull(req.RevuMSI, req.OCRMSI, req.BPXProfile, func(stage string, percent int) {
			s.broadcast(progressEvent{Stage: stage, Percent: percent})
		})
		if err != nil {
			s.broadcast(progressEvent{Stage: "Error: " + err.Error(), Percent: 0, Done: true, Error: err.Error()})
			return
		}
		_ = desktop.CreateShortcut()
		s.broadcast(progressEvent{Stage: "Bluebeam Revu 21 installation complete!", Percent: 100, Done: true})
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func (s *AppServer) handleInstallPrereqs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "another operation is in progress", http.StatusConflict)
		return
	}
	s.busy = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}()

		err := s.bluebeam.InstallPrerequisites(func(stage string, percent int) {
			s.broadcast(progressEvent{Stage: stage, Percent: percent})
		})
		if err != nil {
			s.broadcast(progressEvent{Stage: "Error: " + err.Error(), Percent: 0, Done: true, Error: err.Error()})
			return
		}
		s.broadcast(progressEvent{Stage: "Prerequisites (.NET 4.8, VC++, DXVK, Fonts) installed!", Percent: 100, Done: true})
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func (s *AppServer) handleRepairPostInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BPXProfile string `json:"bpx_profile"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	go func() {
		exePath, ok := s.bluebeam.FindRevuExecutable()
		if !ok {
			s.broadcast(progressEvent{Stage: "Error: Revu.exe not found in prefix.", Percent: 0, Done: true, Error: "Revu.exe not found"})
			return
		}
		extractDir := filepath.Join(s.engine.DriveCPath(), "windows", "temp", "bb_setup_files")
		s.broadcast(progressEvent{Stage: "Re-applying registry & post-install configuration...", Percent: 25})
		_ = s.engine.SetWindows10(nil)
		_ = s.engine.ApplyBluebeamRegistryOverrides(nil)
		s.bluebeam.ConfigurePostInstall(filepath.Dir(exePath), extractDir, req.BPXProfile, func(line string) {
			s.broadcast(progressEvent{Stage: line, Percent: 75})
		})
		s.broadcast(progressEvent{Stage: "Post-install repair complete!", Percent: 100, Done: true})
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func (s *AppServer) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		if err := s.bluebeam.Launch(); err != nil {
			fmt.Fprintf(os.Stderr, "Launch error: %v\n", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"launched": true})
}

func (s *AppServer) handleShortcut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := desktop.CreateShortcut(); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *AppServer) handleWinecfg(w http.ResponseWriter, r *http.Request) {
	go func() {
		_ = s.engine.LaunchWinecfg()
	}()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *AppServer) handleKill(w http.ResponseWriter, r *http.Request) {
	_ = s.engine.KillServer()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *AppServer) handleResetPrefix(w http.ResponseWriter, r *http.Request) {
	_ = s.engine.KillServer()
	_ = os.RemoveAll(s.cfg.PrefixDir)
	_ = os.MkdirAll(s.cfg.PrefixDir, 0755)
	s.cfg.RevuInstalled = false
	s.cfg.RevuExePath = ""
	_ = s.cfg.Save()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *AppServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan progressEvent, 32)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func openAppWindow(url string) {
	appBrowsers := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"brave-browser",
		"microsoft-edge",
		"vivaldi",
	}

	for _, bin := range appBrowsers {
		if path, err := exec.LookPath(bin); err == nil {
			cmd := exec.Command(path, "--app="+url, "--window-size=840,720")
			if err := cmd.Start(); err == nil {
				return
			}
		}
	}

	if path, err := exec.LookPath("xdg-open"); err == nil {
		_ = exec.Command(path, url).Start()
	}
}
