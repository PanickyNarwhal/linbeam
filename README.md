# Linbeam

**Linbeam** is a single-binary Go installer, MSI compatibility patcher, and runtime launcher for running **Bluebeam Revu 21** on `x86_64` Linux workstations over **Proton-GE** and **DXVK**.

---

## Features

- **Zero-Dependency Deployment & Runtime Console**: Embedded desktop web UI and CLI compiled into a single static `x86_64` binary and portable **AppImage**.
- **Automated Proton-GE (`amd64`) Provisioning**: Fetches and verifies `EM_X86_64` GE-Proton builds from GitHub, configures a dedicated 64-bit Windows 10 (Build 19045) prefix, installs DXVK (`d3d11`/`d3d9` over Vulkan), and synthesizes OpenType/TrueType core fonts (`Segoe UI`, `Arial`, `Tahoma`, `Consolas`, `Calibri`) for crisp WPF UI and PDF vector rendering.
- **Official MSI Download & On-the-Fly Wine Patching**: Pulls the official 64-bit deployment archive directly from `https://bluebeam.com/MSIdeployx64` on the user's machine, unpacks `Bluebeam Revu x64 21.msi` and `BluebeamOCR x64 21.msi`, provisions VC++ 2015-2022 and `.NET Framework 4.8`, strips incompatible InstallShield PowerShell custom actions via Win32 `msi.dll`, unpacks `WebView2 FixedVersionRuntime`, and initializes `Safe.enx`.
- **Native Browser OAuth Sign-In (`revusignin://`)**: Configures Revu 21's built-in `SystemBrowser` OAuth PKCE authentication mode with clean host browser handoff and XDG `x-scheme-handler/revusignin` callback routing for Named-User licensing and Bluebeam Studio.
- **In-Place Stack Updater**: Pulls the latest `Linbeam` AppImage or binary, updates `GE-Proton`, and refreshes prefix/desktop integrations in one step.

---

## Quick Start (AppImage)

1. Download **`Linbeam-x86_64.AppImage`** from the [Releases](https://github.com/PanickyNarwhal/linbeam/releases) page.
2. Make it executable and launch the Deployment & Runtime Console:
   ```bash
   chmod +x Linbeam-x86_64.AppImage
   ./Linbeam-x86_64.AppImage
   ```
3. Follow the 3 stages in the console:
   - **Stage 01 (`Proton-GE Runtime Selection`)**: Select an existing `x86_64` Proton-GE runner or click **Fetch Latest GE-Proton (amd64)**.
   - **Stage 02 (`Bluebeam Revu 21 MSI Installer & Patcher`)**: Click **Execute Full Patched Deployment** (automatically downloads and unpacks `bluebeam.com/MSIdeployx64` if no local MSI is specified).
   - **Stage 03 (`Linbeam Runtime Launcher`)**: Click **Install Desktop Launcher** and **Launch Bluebeam Revu 21**.

---

## CLI Usage

```text
linbeam                          Open the Deployment & Runtime Console UI
linbeam setup [path-to-revu.msi] Run guided CLI installation (Proton-GE + .NET 4.8 + MSI patch + Revu 21)
linbeam update [--full]          Pull latest launcher/AppImage, Proton-GE (amd64), and update prefix/shortcuts
linbeam download                 Pull and unpack official Bluebeam x64 MSI ZIP (bluebeam.com/MSIdeployx64)
linbeam launch [file.pdf ...]    Launch Bluebeam Revu 21 directly (or forward revusignin:// OAuth callbacks)
linbeam status                   Show runner, prefix, and payload detection status
linbeam proton                   List or download latest GE-Proton runner
linbeam prereqs                  Install prefix prerequisites only (.NET 4.8, VC++, DXVK, Fonts)
linbeam repair                   Re-run WebView2, Safe.enx, and SystemBrowser OAuth post-install setup
linbeam winetricks <verbs...>    Run winetricks inside the Proton-GE prefix
linbeam winecfg                  Open winecfg for the Proton-GE prefix
linbeam shortcut                 Create Linux desktop shortcut & PDF/OAuth/Studio associations
linbeam kill                     Stop all Wine processes in the prefix
linbeam reset                    Delete and reset the Wine prefix
```

---

## Host Requirements

- **Architecture**: `x86_64` / `amd64` Linux workstation with Vulkan-capable GPU drivers (AMD RADV, Intel ANV, or NVIDIA proprietary).
- **Standard Utilities**: `tar`, `xdg-utils` (`xdg-open`, `xdg-mime`), `cabextract` (used by `winetricks dotnet48`), and `7z` (`p7zip-full` / `7zip`, optional—falls back to bundled `7z.exe` inside Proton).
- **License**: A valid Bluebeam account / Named-User subscription is required to sign in and activate Bluebeam Revu 21.

---

## Building from Source

Requires Go 1.22+:

```bash
# Run unit tests
go test -v ./...

# Build static Linux amd64 binary
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o linbeam .

# Build portable Linbeam-x86_64.AppImage (on Linux)
./scripts/build-appimage.sh
```

---

## License & Legal Notice

Linbeam is open-source software licensed under the [MIT License](LICENSE).

Linbeam is an independent compatibility and deployment tool and is **not** affiliated with, endorsed by, or sponsored by Bluebeam, Inc. or the Nemetschek Group. *Bluebeam* and *Revu* are registered trademarks of Bluebeam, Inc. This repository does **not** host or redistribute any proprietary Bluebeam installers, binaries, or license keys; all official installation packages are fetched directly from Bluebeam's servers by the end user, and activation requires a legitimate Bluebeam subscription.
