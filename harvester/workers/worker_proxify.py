import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "Proxify.vn"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Fetches verified high-speed SOCKS5 proxies from Proxify.vn API.
    """
    url = "https://api.proxify.vn/api/proxy-free"
    headers = {
        "User-Agent": DEFAULT_USER_AGENT,
        "Referer": "https://proxify.vn/"
    }
    
    try:
        r = requests.get(url, headers=headers, timeout=12)
        if r.status_code != 200:
            return
            
        data = r.json()
        proxies = data.get("data", {}).get("proxies", [])
        
        batch = []
        for item in proxies:
            proto = str(item.get("protocol", "")).lower()
            if proto != "socks5" or not item.get("alive") or not item.get("ssl"):
                continue
                
            ip_data = item.get("ip_data", {})
            batch.append({
                "ip": str(item.get("ip", "")).strip(),
                "port": str(item.get("port", "")).strip(),
                "country": str(ip_data.get("country", "")).strip(),
                "country_code": str(ip_data.get("countryCode", "")).strip(),
                "city": str(ip_data.get("city", "")).strip(),
                "source": "proxify.vn"
            })
            
        if batch:
            print(f"[{NAME}] loaded {len(batch)} candidates")
            yield batch
    except Exception as e:
        print(f"[{NAME}] error: {e}")
