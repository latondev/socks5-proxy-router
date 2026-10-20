import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "Proxora"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Paginates through Proxora SOCKS5 API page by page.
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    page = 1
    max_pages = 5
    
    while page <= max_pages:
        url = f"https://api.proxora.io/api/tools/free-proxies?protocol=socks5&per_page=100&page={page}"
        try:
            r = requests.get(url, headers=headers, timeout=12)
            if r.status_code != 200:
                break
                
            data = r.json()
            items = data.get("items", []) if isinstance(data, dict) else (data if isinstance(data, list) else [])
            if not items:
                break
                
            page_batch = []
            for item in items:
                ip = item.get("host") or item.get("ip")
                port = item.get("port")
                if not ip or not port:
                    continue
                    
                page_batch.append({
                    "ip": str(ip).strip(),
                    "port": str(port).strip(),
                    "country": str(item.get("country", "")).strip(),
                    "source": "proxora"
                })
                
            if page_batch:
                print(f"[{NAME}] Page {page}: fetched {len(page_batch)} candidates")
                yield page_batch
                
            if len(items) < 100:
                break
                
            page += 1
        except Exception as e:
            print(f"[{NAME}] error on page {page}: {e}")
            break
