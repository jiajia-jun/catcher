<p align="center">
  <img src="logo/logo-wordmark-1024x256.png" width="440" alt="Catcher">
</p>

<p align="center"><a href="README.md">简体中文</a> | <a href="README.en.md">English</a></p>

<p align="center">A local desktop app that collects assignments from Yuketang, Chaoxing, TronClass, and Pigai into one window</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-1.3.0-4c6ef5">
  <img alt="platform" src="https://img.shields.io/badge/platform-Windows%2010%2F11%20x64%2Farm64-4c6ef5">
  <img alt="local-first" src="https://img.shields.io/badge/local--first-data%20stays%20local-4c6ef5">
  <img alt="no third-party" src="https://img.shields.io/badge/no%20third--party-no%20relay%20server-4c6ef5">
  <img alt="license" src="https://img.shields.io/badge/license-MIT-4c6ef5">
  <a href="https://github.com/jiajia-jun/catcher/releases/latest"><img alt="download latest" src="https://img.shields.io/badge/download-latest-4c6ef5"></a>
</p>

## Supported platforms

| Platform | Login | Status |
| --- | --- | --- |
| Yuketang | QR code | Experimental (not all assignment types covered) |
| Chaoxing | QR code / phone number + password | Verified on real devices |
| TronClass | Import browser session | Verified on real devices |
| Pigai | WeChat QR code | Experimental (pagination untested) |

## Features

- One assignment table showing platform, course, status, deadline, and deadline source
- Search assignments by course
- Click a platform card to show only that platform's assignments; click again to clear
- Each platform card has a fetch checkbox; the selection is saved to the config file, and the overview supports selecting all at once
- Refresh per platform, with cancel and progress; one platform failing does not affect the other
- Show the changelog on the first launch after an upgrade; reopen it from settings
- The platform overview sits on a single row; hover it and scroll the wheel to move between platform cards
- **Manual marking** for platforms that do not expose submission status; restore the platform value at any time
- Check for updates on startup, prompt when a new version is available, and open the GitHub release page
- All data stays on this machine and is **never uploaded to any server**

## Installation

Requirements: Windows 10/11, x64 or ARM64. WebView2 Runtime is required: the installer handles it automatically; the portable build requires it to be present (bundled with Windows 11).

Download from the [Releases](https://github.com/jiajia-jun/catcher/releases/latest) page; two package types are available:

- **Installer** (x64 only): `Catcher-<version>-amd64-installer.exe`, installs to the current user's directory by default, **no administrator rights required**; you can pick any directory writable by the current user (including D:, E:, and other non-system drives)
- **Portable**: `Catcher-windows-amd64.zip` (x64) or `Catcher-windows-arm64.zip` (ARM64); extract to any writable directory and run `Catcher.exe`

> The install directory must be writable by the current user. Directories such as `Program Files` are not writable for normal users; the installer blocks them and asks for another location.

### Uninstall and update

#### Uninstall

The uninstaller is located **in the install directory**. It asks whether to delete data as well. If you choose to keep it, assignment records, login sessions, and logs remain in `data/` under the install directory, and **reinstalling to the same directory lets you continue**.  
The install directory itself is not deleted: even if you choose to delete data, only program files are removed and the directory stays in place.

#### Update

Install the new version into the **same directory** as the old one.

<details>
<summary>Data directory</summary>

Defaults to `data/` next to the executable. It can also be set with:

1. CLI flag `--data-dir <path>`
2. Environment variable `CATCHER_DATA_DIR`

The directory is created automatically if missing; if it is not writable the app fails explicitly and **never falls back to the C: drive or a temporary directory**.

```text
data/
├── catcher.yaml
├── catcher.db
├── catcher.db-wal
├── catcher.db-shm
└── logs/
    └── catcher.log
```

Installing on a local disk is recommended. SQLite file locking is unreliable on network and mapped drives and may corrupt data.

</details>

## Security and privacy

- The app **does not listen on any local port** and runs no background service; it only makes outbound requests it initiates
- Yuketang, Chaoxing, and Pigai use HTTPS; the TronClass campus site only offers HTTP, so the session is transmitted in plaintext within that site — **using the campus network or a VPN is recommended**
- Login sessions (including Pigai cookies) and the Chaoxing password are encrypted with Windows DPAPI and stored in local SQLite; only the current Windows user can decrypt them
- Logs **do not record** Cookie, Set-Cookie, Authorization, passwords, tokens, or QR code contents
- The update check only asks GitHub for the latest version number; the request contains only the client version and standard HTTP headers, no account, assignment, or log data
- Data is **never uploaded to any third-party server**

## Known limitations

- **Log level and similar settings are still read-only**: they live in `data/catcher.yaml` and require a restart; the UI only supports editing the platform refresh selection
- **TronClass only supports browser session import**: re-import after the session expires; CAS auto-login is not supported. Import and current-semester fetching have been verified in the real environment; session rotation and similar details are not verified
- **Pigai is experimental**: only the first page of each list is fetched — the pagination parameters are unverified, so older records may be missed when there are many assignments; password login is not supported, and an expired session must be re-scanned
- **Some Chaoxing QR login branches are not verified**: normal scanning (get code, poll, confirm) works on real devices; the intermediate "scanned / cancelled" states and behavior after the QR code expires are not verified; use phone number + password if you run into problems
- **Yuketang is experimental**: it has many assignment types, and this version may not cover some special ones, so fetching may fail for some accounts; session renewal and the 504 path during QR polling are not verified
- **Pigai does not clean up old assignments while pagination is unverified**: each refresh only upserts, so historical data is kept and assignments from left classes may still appear
- The installer and executable are **not code-signed**; Windows SmartScreen may show "Unknown publisher", which is expected

<details>
<summary>Build from source</summary>

Requires Go 1.27, Node 24, and Wails v2.13.

```bash
wails build            # output: build/bin/Catcher.exe
wails build -nsis -installscope user   # also build a per-user installer (requires NSIS)
```

Development mode:

```bash
wails dev              # frontend hot reload; data goes to data/ at the repo root
```

</details>

## Disclaimer

- This is an unofficial personal project, not affiliated with or endorsed by Yuketang, Chaoxing, TronClass, Pigai, or their operators
- The interface docs in this repository were compiled from public pages and packet captures, for learning and technical research only; the interfaces and related content belong to their respective platforms — contact us for removal if anything infringes
- Users must comply with each platform's user agreement, terms of service, and applicable laws; do not use this project for commercial purposes, bulk scraping, disrupting platform services, or other improper uses
- You use this project at your own risk; the author is not liable for data accuracy, service availability, or any damages arising from its use
- Provided "as is" under the MIT License, without warranty of any kind, express or implied

## Notes

This is a personal learning project — **do not use it for illegal purposes**.  
Interface docs are being added gradually; currently published:

- [Chaoxing interface docs](docs/接口文档/学习通.md)

- [Yuketang interface docs](docs/接口文档/雨课堂.md)

<h3 align="center">This project will remain open source forever</h3>
