import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "Geonode"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Paginates through Geonode SOCKS5 public API page by page (100 proxies/page, all pages).
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    page = 1
    max_pages = 20  # Dynamically updated from API total
    
    while page <= max_pages:
        url = f"https://proxylist.geonode.com/api/proxy-list?protocols=socks5&limit=100&page={page}&sort_by=lastChecked&sort_type=desc"
        try:
            r = requests.get(url, headers=headers, timeout=12)
            if r.status_code != 200:
                break
                
            data = r.json()
            items = data.get("data", [])
            total = data.get("total", 0)
            if total > 0:
                max_pages = (total + 99) // 100
                
            if not items:
                break
                
            page_batch = []
            for item in items:
                ip = item.get("ip")
                port = item.get("port")
                if not ip or not port:
                    continue
                    
                page_batch.append({
                    "ip": str(ip).strip(),
                    "port": str(port).strip(),
                    "country": str(item.get("country", "")).strip(),
                    "city": str(item.get("city", "")).strip(),
                    "source": "geonode",
                    "total_pages": max_pages,
                    "page": page
                })
                
            if page_batch:
                print(f"[{NAME}] Page {page}/{max_pages}: fetched {len(page_batch)} candidates (Total in source: {total})")
                yield page_batch
                
            # If fewer items than limit, reached the last page
            if len(items) < 100:
                break
                
            page += 1
        except Exception as e:
            print(f"[{NAME}] error on page {page}: {e}")
            break
