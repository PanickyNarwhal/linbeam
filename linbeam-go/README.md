# Linbeam (Go + Proton-GE Edition)

A self-contained, zero-dependency Go installer and launcher for running **Bluebeam Revu 21** on Linux via **Proton-GE**, modeled on [`keryx`](https://github.com/PanickyNarwhal/keryx).

## Features
- **Single Static Binary**: Embeds the guided dark-mode Web UI (`ui/index.html`) and C# MSI database patcher (`pkg/installer/msi_patcher.cs`) directly into a single executable (`linbeam`).
- **Standalone Proton-GE Engine**: Automatically discovers installed Proton-GE / Wine runners or downloads the latest `GE-Proton` release from GitHub without requiring Steam.
- **Hardware-Accelerated Rendering & WPF Font Synthesis**: Configures Proton-GE's bundled DXVK (`d3d11`, `d3d9`, `dxgi` over Vulkan) and synthesizes core Windows TrueType fonts (`Segoe UI`, `Arial`, `Tahoma`, `Consolas`, `Calibri`, etc.) so WPF never crashes.
- **Automated Bluebeam Revu 21 MSI Patching**: Compiles and runs the embedded `msi_patcher.cs` inside the Proton-GE prefix to remove the broken InstallShield PowerShell custom actions (`Set_REBOOT_NEEDED_Prop_PS`, `Set_LANGUAGE_CHOICE_Prop_PS`, `ExpandWebView2`) and extract `Bluebeam Admin Install.exe`.
- **WebView2 & `Safe.enx` Post-Install Setup**: Extracts the bundled `Microsoft.WebView2.FixedVersionRuntime.x64.7z` into `Revu\WebView2Runtime`, configures `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS="--no-sandbox --disable-gpu"` for Bluebeam Studio login, generates `Safe.enx`, and imports any `.bpx` profile.

## Quick Start on Linux

A pre-compiled Linux `amd64` binary (`linbeam`) is included right in this folder.

```bash
cd linbeam-go
chmod +x ./linbeam

# Launch the Guided Web UI Installer & Manager:
./linbeam

# Or run the automated CLI setup (auto-detects ../payloads):
./linbeam setup

# Launch Bluebeam Revu 21 directly:
./linbeam launch
```

## Building from Source

```bash
cd linbeam-go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o linbeam .
```
