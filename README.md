# Linbeam 
Linbeam is a Go launcher used for installing the program Bluebeam Revu 21 onto Linux using Proton-GE.

> [!WARNING]
> As with all my other projects, AI should be used as a tool. I built the framework of the code, then gave it to my LLM and guided it to clean up and edit the rest of the code. 

### What Works?
---
* Bluebeam Revu 21 & OCR 21 automated downnload, patching, and installation using the Bluebeam MSI package. 
* User Licensing and Bluebeam sign-in over a OAuth PKCE flow.
* Hardware Accelerated PDF Rendering (Direct3D 11/9 & Vulkan via DXVK)
* WPF UI and PDF typography using the OpenType/TrueType font families.
* Custom .bpx Profiles
* Desktop Integration and Updates

## System Requirements
---
- **CPU Architecture**: `x86_64` / `amd64` (AMD Ryzen or Intel core).
- **Graphics**: Vulkan-capable GPU driver (`mesa-vulkan-drivers` / RADV for AMD, ANV for Intel, or proprietary NVIDIA driver).
- **Host Packages**:
  - `tar`, `curl`, `xdg-utils` (`xdg-open`, `xdg-mime`)
  - `cabextract` (required by Winetricks when installing `.NET Framework 4.8`)
  - `7z` / `p7zip-full` (optional; falls back to the `7z.exe` bundled in the MSI)
- **Bluebeam Account**: An active Bluebeam Named-User subscription is required to sign in and activate Revu 21.

### Installing Host Dependencies

**Ubuntu / Debian / Linux Mint:**
```bash
sudo apt update && sudo apt install -y cabextract p7zip-full xdg-utils vulkan-tools
```
**Fedora:**
```bash
sudo dnf install -y cabextract p7zip p7zip-plugins xdg-utils vulkan-tools
```
**Arch Linux:**
```bash
sudo pacman -S --needed cabextract 7zip xdg-utils vulkan-tools
```


## Quick Start (AppImage)
---
1. Download the latest binary from [Releases](https://github.com/PanickyNarwhal/linbeam/releases) 

2. Make it executable
`chmod +x Linbeam-x86_64.AppImage` 
`./Linbeam-x86_64.AppImage`

3. Follow the steps in the launcher

## From Terminal
----
chmod the launcher (`linbeam-linux-amd64`) 
then run it in terminal and use the CLI Commands 

## Building from Source (you monster.)
Requires Go 1.22 or newer:
```bash
git clone https://github.com/PanickyNarwhal/linbeam.git
cd linbeam
go test -v ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o linbeam .
./scripts/build-appimage.sh
```

#### CLI Commands
| Command | Description |
| :--- | :--- |
| `linbeam` | Open the Deployment & Runtime Console UI |
| `linbeam setup [path-to-revu.msi]` | Run full guided CLI installation |
| `linbeam launch [file.pdf ...]` | Launch Bluebeam Revu 21 (or pass a PDF / `revusignin://` URI) |
| `linbeam update [--full]` | Update Linbeam, Proton-GE (`amd64`), prefix overrides, and shortcuts (`--full` also re-pulls the latest Bluebeam MSI) |
| `linbeam download` | Download and unpack the official ZIP from `bluebeam.com/MSIdeployx64` |
| `linbeam status` | Show detected runner, prefix state, and unpacked MSI payloads |
| `linbeam repair` | Re-apply registry overrides, `Safe.enx`, `WebView2`, and OAuth configuration |
| `linbeam shortcut` | Install `.desktop` entries and register PDF / `revusignin://` MIME handlers |
| `linbeam winecfg` | Open `winecfg` for the Linbeam prefix |
| `linbeam winetricks <verbs...>` | Run Winetricks verbs inside the Linbeam prefix |
| `linbeam kill` | Stop all running Wine processes in the prefix |
| `linbeam reset` | Wipe and recreate the Wine prefix (`~/.local/share/linbeam/prefix`) |

## Disclaimer & License
Released under the [MIT License](LICENSE).
Linbeam is an independent open-source compatibility tool and is **not** affiliated with, endorsed by, or sponsored by Bluebeam, Inc. or the Nemetschek Group. *Bluebeam* and *Revu* are registered trademarks of Bluebeam, Inc.
This project does **not** distribute any proprietary Bluebeam code, installers, or license keys. Official MSI packages are downloaded directly from Bluebeam's servers onto the user's machine at install time, and use of Bluebeam Revu 21 requires a legitimate Bluebeam subscription.

