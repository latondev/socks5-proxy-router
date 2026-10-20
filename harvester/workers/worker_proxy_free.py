import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "Proxy-Free"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Fetches verified proxies from Proxy-Free JSON API.
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    url = "https://proxy-free.com/free-api/proxies/?format=json"
    
    try:
        r = requests.get(url, headers=headers, timeout=12)
        if r.status_code != 200:
            return
            
        data = r.json()
        proxies = data.get("proxies", [])
        batch = []
        for p in proxies:
            ip = p.get("ip")
            port = p.get("port")
            if not ip or not port:
                continue
                
            batch.append({
                "ip": str(ip).strip(),
                "port": str(port).strip(),
                "country": str(p.get("country", "")).strip(),
                "country_code": str(p.get("country_code", "")).strip(),
                "source": "proxy-free"
            })
            
        if batch:
            print(f"[{NAME}] fetched {len(batch)} candidates")
            yield batch
    except Exception as e:
        print(f"[{NAME}] error: {e}")
