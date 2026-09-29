# SOCKS5 Proxy Router & Pool

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-blue)](#quick-start)

A high-performance, self-rotating SOCKS5 proxy pool and routing gateway built in Go with **zero external dependencies** (standard library only). 

The system automatically collects free high-speed SOCKS5 proxies from multiple curated upstream sources, filters out blocked regions, performs strict Google TLS connectivity health checks, and presents a single local or remote SOCKS5 endpoint that auto-rotates upstream proxies on an intelligent interval or on connection failures. It also features a sleek, real-time dark Web Console for monitoring, instant testing, and manual proxy control.

---

## Key Highlights

- **Multi-Source Scraping**: Concurrently gathers proxies from high-volume, verified feeds (`Proxify.vn`, `Monosans`, and `socks5-proxy.github.io`), aggregating 1,500+ candidates per cycle.
- **Strict Connectivity & TLS Verification**: Concurrently verifies connectivity by performing direct Google TLS handshakes (`google.com:443`) through each candidate.
- **Latency Filtering**: Filters candidates by customizable latency caps (default: `< 2500ms`) and sorts the pool from fastest to slowest.
- **Geo-Location & Flag Resolution**: Resolves IP regions, cities, and countries, automatically attaching country flag emojis and filtering unwanted regions.
- **Smart Rotation & Auto-Failover**:
  - Automatically rotates active proxy every 3 to 6 minutes (jittered).
  - Automatically fails over to the next healthy proxy if an upstream connection fails (up to 3 retries).
- **Modern Web Console (GUI)**:
  - 2-column split-pane control center (responsive, dark-mode design).
  - Pinned active route bar showing latency gauge, country flag, and rotation countdown.
  - Interactive one-click TLS handshake tester with round-trip latency reporting.
  - Proxy exporter (plain text or JSON format).
  - Instant client snippet generator for cURL, Python, Environment variables, Git, and Telegram.
  - Switch between Grid View and Dense Table View.
- **Zero External Dependencies**: Pure Go standard library. Compact Docker image (< 20MB).

---

## System Architecture

```text
[ Client Application ] (Browser, Python, Bot, Git, etc.)
         |
         | SOCKS5 Request (:1080)
         v
+-------------------------------------------------------+
|  SOCKS5 Proxy Router (Go Service)                     |
|                                                       |
|  - Inbound SOCKS5 Handshake & Target Demuxing         |
|  - Active Proxy Selector & Auto-Failover Router       |
|                                                       |
|  [ Proxy Pool ] <--- [ Health Checker & Geo Lookup ]  |
|         ^                     ^                       |
|         |                     |                       |
|  [ Auto-Rotator ]     [ Multi-Source Scraper ]        |
|  (Every 3-6 mins)     (Proxify, Monosans, Github)     |
+-------------------------------------------------------+
         |
         | Upstream SOCKS5 Tunnel
         v
[ Fastest Active Proxy ] ---> [ Target Host (Google, Web, APIs) ]
```

---

## Quick Start

### Option 1: Docker Compose (Recommended for VPS / Server)

Clone the repository and launch the container with Docker Compose:

```bash
# Clone repository
git clone https://github.com/latondev/socks5-proxy-router.git
cd socks5-proxy-router

# Launch with Docker Compose
docker compose up -d --build
```

The service will start automatically in the background with auto-restart enabled:
- **SOCKS5 Gateway**: `0.0.0.0:1080`
- **Web Console**: `http://localhost:8080` (or `http://YOUR_VPS_IP:8080`)

To view live service logs:
```bash
docker compose logs -f
```

---

### Option 2: Run from Source (Go 1.22+)

```bash
# Clone repository
git clone https://github.com/latondev/socks5-proxy-router.git
cd socks5-proxy-router

# Build binary
go build -ldflags="-s -w" -o socks5-pool .

# Run with default settings (Localhost:1080 SOCKS5, Localhost:8080 Web Console)
./socks5-pool

# Or run with custom parameters
./socks5-pool -listen 0.0.0.0:1080 -status 0.0.0.0:8080 -max-latency 2s
```

On Windows:
```powershell
.\socks5-pool.exe -listen 0.0.0.0:1080 -status 0.0.0.0:8080
```

---

## Configuration & CLI Flags

The router can be customized using command-line arguments:

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-listen` | `127.0.0.1:1080` | Local or public SOCKS5 listen address (`0.0.0.0:1080` for public) |
| `-status` | `127.0.0.1:8080` | Web Console and REST API address |
| `-url` | `auto` | Proxy source URL, local file path, or `auto` for multi-source scraping |
| `-scrape-interval` | `20m` | Time interval between background scraping cycles |
| `-check-timeout` | `4s` | Connection timeout per proxy candidate check |
| `-max-latency` | `2500ms` | Maximum acceptable latency for proxies added to the pool |
| `-max-concurrent` | `40` | Maximum concurrent health checks running in parallel |

---

## Web Console & REST API

Visit `http://localhost:8080` (or `http://YOUR_IP:8080`) in your browser to access the control center.

### Web Console Features

- **Route Overview**: View current active proxy, region, country, IP address, and ping.
- **Manual Proxy Selection**: Click any proxy card or row to immediately route all future traffic through that proxy.
- **Live TLS Ping**: Press the **Test Handshake** button to run an on-demand TLS test through the active proxy to verify external connectivity and measure round-trip time.
- **Pool Refresh**: Trigger an instant re-scrape and health check sweep across all sources.
- **Export**: One-click download of all healthy proxies in Plain Text (`IP:Port`) or detailed JSON format.

### API Reference

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/api/status` | `GET` | Returns full pool status JSON (total count, active proxy, metrics, proxy list). |
| `/api/switch` | `GET` | Rotates to the next proxy in the pool. |
| `/api/switch?index=N` | `GET` | Switches route to proxy at zero-based index `N`. |
| `/api/refresh` | `POST` | Triggers background re-scraping and health checks. |
| `/api/test` | `GET` | Performs real-time Google TLS handshake through the active proxy. |
| `/api/export?format=text` | `GET` | Exports active proxy list in raw plain text format (`host:port\n`). |
| `/api/export?format=json` | `GET` | Exports active proxy list in full JSON array format. |

---

## Client Configuration Examples

Once the router is running on your local machine or remote VPS (`160.187.229.140`), configure your applications as follows:

### 1. cURL (Command Line)
```bash
# SOCKS5 with remote DNS resolution (recommended)
curl.exe -x socks5h://127.0.0.1:1080 https://api.ipify.org?format=json
```

### 2. Python (Requests)
```python
import requests

# Requires requests[socks]: pip install requests[socks]
proxies = {
    'http': 'socks5h://127.0.0.1:1080',
    'https': 'socks5h://127.0.0.1:1080'
}

response = requests.get('https://api.ipify.org?format=json', proxies=proxies, timeout=10)
print("Routed IP:", response.json())
```

### 3. Node.js (Axios)
```javascript
import axios from 'axios';
import { SocksProxyAgent } from 'socks-proxy-agent';

const agent = new SocksProxyAgent('socks5h://127.0.0.1:1080');

const res = await axios.get('https://api.ipify.org?format=json', {
  httpAgent: agent,
  httpsAgent: agent,
  timeout: 10000
});

console.log('Routed IP:', res.data);
```

### 4. Terminal & Environment Variables
```bash
# Linux / macOS / Git Bash
export all_proxy="socks5://127.0.0.1:1080"
export http_proxy="socks5://127.0.0.1:1080"
export https_proxy="socks5://127.0.0.1:1080"

# Windows PowerShell
$env:all_proxy="socks5://127.0.0.1:1080"
$env:http_proxy="socks5://127.0.0.1:1080"
$env:https_proxy="socks5://127.0.0.1:1080"
```

### 5. Git CLI
```bash
git config --global http.proxy 'socks5h://127.0.0.1:1080'
git config --global https.proxy 'socks5h://127.0.0.1:1080'
```

### 6. Web Browser (Chrome / Edge / Firefox)
- Install **Proxy SwitchyOmega** (or configure system proxy settings).
- Create a SOCKS5 profile with:
  - **Protocol**: `SOCKS5`
  - **Server**: `127.0.0.1` (or your VPS IP)
  - **Port**: `1080`
  - **Username / Password**: Leave blank

### 7. Telegram Messenger
- Go to **Settings** > **Advanced** > **Connection type** > **Custom proxy** > **Add proxy**.
- Select **SOCKS5**.
- Enter **Hostname**: `127.0.0.1` (or your VPS IP) and **Port**: `1080`.

---

## Repository Structure

```text
├── checker.go         # Concurrent proxy health checks & Google TLS verification
├── config.go          # CLI configuration parsing and environment handling
├── docker-compose.yml # 1-click Docker Compose production setup
├── Dockerfile         # Multi-stage lightweight Alpine build (< 20MB)
├── go.mod             # Go module definition
├── main.go            # Service orchestration, background scraper & rotation loops
├── pool.go            # Thread-safe proxy pool management & index rotation
├── scraper.go         # Multi-source scraper (Proxify, Monosans, socks5-proxy)
├── server.go          # Pure Go SOCKS5 RFC1928 protocol engine & TCP relay
├── status.go          # Enterprise Dark Web Console & REST API handlers
└── start.bat          # Quick-launch batch script for Windows users
```

---

## Production Deployment on VPS

To deploy on a remote Linux server (Ubuntu / Debian / CentOS):

1. **Ensure Docker is installed**:
   ```bash
   curl -fsSL https://get.docker.com | sh
   ```
2. **Transfer files to the server**:
   ```bash
   scp -r . root@YOUR_VPS_IP:/opt/socks5-proxy-router
   ```
3. **Start the service**:
   ```bash
   ssh root@YOUR_VPS_IP "cd /opt/socks5-proxy-router && docker compose up -d --build"
   ```
4. **Manage service**:
   - Check container status: `docker compose ps`
   - Stream logs: `docker compose logs -f`
   - Restart service: `docker compose restart`
   - Stop service: `docker compose down`

---

## License

This project is licensed under the [MIT License](LICENSE). Feel free to use, modify, and distribute for personal or commercial projects.
