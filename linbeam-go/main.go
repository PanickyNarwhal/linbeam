package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/desktop"
	"github.com/PanickyNarwhal/linbeam/pkg/installer"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	engine := proton.NewEngine(cfg)
	bm := installer.NewBluebeamManager(cfg, engine)
	app := NewAppServer(cfg, engine, bm)

	args := os.Args[1:]
	if len(args) == 0 {
		if err := app.StartGUI(); err != nil {
			fmt.Fprintf(os.Stderr, "GUI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Direct PDF / BPX / Studio protocol launch
	firstLower := strings.ToLower(args[0])
	if strings.HasSuffix(firstLower, ".pdf") ||
		strings.HasSuffix(firstLower, ".bpx") ||
		strings.HasPrefix(firstLower, "bluebeam:") ||
		strings.HasPrefix(firstLower, "bbstudio:") {
		if err := bm.Launch(args...); err != nil {
			fmt.Fprintf(os.Stderr, "Launch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cmd := args[0]
	switch cmd {
	case "--gui", "gui", "manage":
		if err := app.StartGUI(); err != nil {
			fmt.Fprintf(os.Stderr, "GUI error: %v\n", err)
			os.Exit(1)
		}

	case "launch", "run":
		if err := bm.Launch(args[1:]...); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to launch Bluebeam Revu 21: %v\n", err)
			os.Exit(1)
		}

	case "setup", "install":
		var customMsi string
		if len(args) > 1 {
			customMsi = args[1]
		}
		runCLISetup(cfg, engine, bm, customMsi)

	case "proton":
		runCLIProton(cfg, engine)

	case "prereqs":
		err := bm.InstallPrerequisites(func(stage string, pct int) {
			fmt.Printf("[%3d%%] %s\n", pct, stage)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Prerequisites failed: %v\n", err)
			os.Exit(1)
		}

	case "repair":
		exePath, ok := bm.FindRevuExecutable()
		if !ok {
			fmt.Fprintln(os.Stderr, "Revu.exe not found in prefix. Run 'linbeam setup' first.")
			os.Exit(1)
		}
		extractDir := filepath.Join(engine.DriveCPath(), "windows", "temp", "bb_setup_files")
		_ = engine.SetWindows10(nil)
		_ = engine.ApplyBluebeamRegistryOverrides(nil)
		payloads := cfg.DiscoverPayloads()
		bm.ConfigurePostInstall(filepath.Dir(exePath), extractDir, payloads.BPXProfile, func(line string) {
			fmt.Println(line)
		})
		fmt.Println("Post-install repair complete.")

	case "winetricks":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: linbeam winetricks <verb1> [verb2...]")
			os.Exit(1)
		}
		if err := engine.RunWinetricks(func(line string) { fmt.Println(line) }, args[1:]...); err != nil {
			fmt.Fprintf(os.Stderr, "Winetricks error: %v\n", err)
			os.Exit(1)
		}

	case "winecfg":
		if err := engine.LaunchWinecfg(); err != nil {
			fmt.Fprintf(os.Stderr, "winecfg error: %v\n", err)
			os.Exit(1)
		}

	case "shortcut":
		if err := desktop.CreateShortcut(); err != nil {
			fmt.Fprintf(os.Stderr, "Shortcut error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Desktop shortcut created at ~/.local/share/applications/linbeam.desktop")

	case "kill":
		_ = engine.KillServer()
		fmt.Println("Stopped all Wine processes in prefix.")

	case "reset":
		_ = engine.KillServer()
		_ = os.RemoveAll(cfg.PrefixDir)
		_ = os.MkdirAll(cfg.PrefixDir, 0755)
		cfg.RevuInstalled = false
		cfg.RevuExePath = ""
		_ = cfg.Save()
		fmt.Println("Prefix reset complete.")

	case "status":
		printStatus(cfg, engine, bm)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runCLISetup(cfg *config.Config, engine *proton.Engine, bm *installer.BluebeamManager, customRevuMsi string) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("====================================================")
	fmt.Println("   Linbeam — Bluebeam Revu 21 Guided Linux Setup    ")
	fmt.Println("====================================================")

	// 1. Check or download Proton-GE
	runners, _ := engine.DiscoverRunners()
	if len(runners) == 0 {
		fmt.Println("\n[1/3] No Proton-GE runner found. Downloading latest GE-Proton from GitHub...")
		_, err := engine.DownloadLatestGEProton(func(stage string, pct int) {
			fmt.Printf("\r[%3d%%] %-60s", pct, stage)
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to download Proton-GE: %v\n", err)
			os.Exit(1)
		}
	} else {
		active, _ := engine.GetActiveRunner()
		fmt.Printf("\n[1/3] Using Proton-GE runner: %s (%s)\n", active.Name, active.Path)
	}

	// 2. Discover or prompt for Bluebeam Revu 21 MSI
	payloads := cfg.DiscoverPayloads()
	revuMsi := customRevuMsi
	if revuMsi == "" {
		revuMsi = payloads.RevuMSI
	}

	if revuMsi == "" {
		fmt.Print("\nEnter path to 'Bluebeam Revu x64 21.msi': ")
		input, _ := reader.ReadString('\n')
		revuMsi = strings.Trim(strings.TrimSpace(input), `"'`)
	} else {
		fmt.Printf("\n[2/3] Detected Bluebeam Revu 21 MSI: %s\n", revuMsi)
		if payloads.OCRMSI != "" {
			fmt.Printf("      Detected Bluebeam OCR 21 MSI : %s\n", payloads.OCRMSI)
		}
		if payloads.BPXProfile != "" {
			fmt.Printf("      Detected Bluebeam .bpx Profile: %s\n", payloads.BPXProfile)
		}
	}

	// 3. Run full install pipeline
	fmt.Println("\n[3/3] Starting Proton-GE prefix setup, MSI patching, and Bluebeam Revu 21 installation...")
	err := bm.InstallFull(revuMsi, payloads.OCRMSI, payloads.BPXProfile, func(stage string, pct int) {
		fmt.Printf("[%3d%%] %s\n", pct, stage)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nInstallation failed: %v\n", err)
		os.Exit(1)
	}

	if err := desktop.CreateShortcut(); err == nil {
		fmt.Println("Created desktop shortcut (~/.local/share/applications/linbeam.desktop)")
	}

	fmt.Println("\nSetup complete! Launch Bluebeam Revu 21 anytime with:")
	fmt.Println("  ./linbeam launch")
}

func runCLIProton(cfg *config.Config, engine *proton.Engine) {
	runners, _ := engine.DiscoverRunners()
	if len(runners) == 0 {
		fmt.Println("No runners found. Downloading latest GE-Proton...")
		_, err := engine.DownloadLatestGEProton(func(stage string, pct int) {
			fmt.Printf("\r[%3d%%] %-60s", pct, stage)
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println("Discovered Proton/Wine runners:")
	for i, r := range runners {
		mark := " "
		if r.Name == cfg.SelectedRunner || (cfg.SelectedRunner == "" && i == 0) {
			mark = "*"
		}
		fmt.Printf(" %s [%d] %s (%s) -> %s\n", mark, i+1, r.Name, r.Source, r.Path)
	}
}

func printStatus(cfg *config.Config, engine *proton.Engine, bm *installer.BluebeamManager) {
	runner, _ := engine.GetActiveRunner()
	exePath, installed := bm.FindRevuExecutable()
	payloads := cfg.DiscoverPayloads()

	fmt.Println("Linbeam Status")
	fmt.Println("--------------")
	fmt.Printf("Data Dir        : %s\n", cfg.DataDir)
	fmt.Printf("Prefix Dir      : %s\n", cfg.PrefixDir)
	if runner != nil {
		fmt.Printf("Active Runner   : %s (%s)\n", runner.Name, runner.Path)
	} else {
		fmt.Println("Active Runner   : None (run 'linbeam proton' or 'linbeam --gui')")
	}
	fmt.Printf("Revu Installed  : %v\n", installed)
	if installed {
		fmt.Printf("Revu Executable : %s\n", exePath)
	}
	fmt.Printf(".NET 4.8 Ready  : %v\n", bm.HasDotNet48())
	fmt.Println("\nDiscovered Payloads:")
	fmt.Printf("  Revu 21 MSI   : %s\n", orNone(payloads.RevuMSI))
	fmt.Printf("  OCR 21 MSI    : %s\n", orNone(payloads.OCRMSI))
	fmt.Printf("  vc_redist.x64 : %s\n", orNone(payloads.VCRedistX64))
	fmt.Printf("  vc_redist.x86 : %s\n", orNone(payloads.VCRedistX86))
	fmt.Printf("  .bpx Profile  : %s\n", orNone(payloads.BPXProfile))
}

func orNone(s string) string {
	if s == "" {
		return "(not found)"
	}
	return s
}

func printUsage() {
	fmt.Println(`Linbeam — Run Bluebeam Revu 21 on Linux via Proton-GE

Usage:
  linbeam                          Open the guided Web UI installer & manager
  linbeam setup [path-to-revu.msi] Run guided CLI installation (Proton-GE + .NET 4.8 + MSI patch + Revu 21)
  linbeam launch [file.pdf ...]    Launch Bluebeam Revu 21 directly
  linbeam status                   Show runner, prefix, and payload detection status
  linbeam proton                   List or download latest GE-Proton runner
  linbeam prereqs                  Install prefix prerequisites only (.NET 4.8, VC++, DXVK, Fonts)
  linbeam repair                   Re-run WebView2 runtime extraction & Safe.enx post-install setup
  linbeam winetricks <verbs...>    Run winetricks inside the Proton-GE prefix
  linbeam winecfg                  Open winecfg for the Proton-GE prefix
  linbeam shortcut                 Create Linux desktop shortcut & PDF/Studio associations
  linbeam kill                     Stop all Wine processes in the prefix
  linbeam reset                    Delete and reset the Wine prefix`)
}
