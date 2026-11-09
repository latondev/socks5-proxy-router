import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "CharlesPikachu"

ENDPOINTS = [
    "https://raw.githubusercontent.com/CharlesPikachu/freeproxy/master/proxies.json",
    "https://charlespikachu.github.io/freeproxy/proxies.json",
]

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Fetches SOCKS5 proxies from CharlesPikachu/freeproxy daily consolidated feed.
    Contains 5,000+ SOCKS5 proxies updated via GitHub Actions.
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    
    data = None
    for url in ENDPOINTS:
        try:
            r = requests.get(url, headers=headers, timeout=20)
            if r.status_code == 200:
                payload = r.json()
                data = payload.get("data", [])
                if data:
                    print(f"[{NAME}] Successfully loaded feed from {url} (Total entries: {len(data)})")
                    break
        except Exception as e:
            print(f"[{NAME}] error connecting to {url}: {e}")
            continue

    if not data:
        return

    batch = []
    seen = set()
    total_socks5 = 0

    for item in data:
        protocol = str(item.get("protocol", "")).lower()
        if "socks5" not in protocol:
            continue

        ip = str(item.get("ip", "")).strip()
        port = str(item.get("port", "")).strip()
        if not ip or not port:
            continue

        addr = f"{ip}:{port}"
        if addr in seen:
            continue
        seen.add(addr)
        total_socks5 += 1

        batch.append({
            "ip": ip,
            "port": port,
            "country": item.get("country", ""),
            "source": "charlespikachu"
        })

        if len(batch) >= 150:
            yield batch
            batch = []

    if batch:
        yield batch

    print(f"[{NAME}] Dispatched {total_socks5} SOCKS5 candidates to checker")

