import re
import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT, FIRECRAWL_API_KEY

NAME = "FreeProxy.World"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Paginates FreeProxy.World SOCKS5 list (page 1 -> n).
    Uses Firecrawl or direct HTML parsing with fallback.
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    page = 1
    max_pages = 15  # Crawl first 15 pages per cycle
    
    # Try initializing Firecrawl if key is available
    fc_app = None
    if FIRECRAWL_API_KEY:
        try:
            from firecrawl import FirecrawlApp
            fc_app = FirecrawlApp(api_key=FIRECRAWL_API_KEY)
        except Exception:
            pass

    while page <= max_pages:
        url = f"https://www.freeproxy.world/?type=socks5&page={page}"
        page_batch = []
        
        try:
            # 1. Try Firecrawl markdown first if available
            if fc_app:
                res = fc_app.scrape(url, formats=["markdown"])
                md = getattr(res, "markdown", "") or ""
                matches = re.findall(r'\|\s*(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\s*\|\s*\[?(\d{1,5})\]?', md)
                for ip, port in matches:
                    page_batch.append({
                        "ip": ip.strip(),
                        "port": port.strip(),
                        "source": "freeproxy.world",
                        "total_pages": max_pages,
                        "page": page
                    })
                    
            # 2. Direct HTTP fallback if Firecrawl didn't yield
            if not page_batch:
                r = requests.get(url, headers=headers, timeout=12)
                if r.status_code == 200:
                    ips = re.findall(r'<td class="show-ip-div">\s*([\d\.]+)', r.text)
                    ports = re.findall(r'href="/\?port=(\d+)"', r.text)
                    for i in range(min(len(ips), len(ports))):
                        page_batch.append({
                            "ip": ips[i].strip(),
                            "port": ports[i].strip(),
                            "source": "freeproxy.world",
                            "total_pages": max_pages,
                            "page": page
                        })
                        
            if page_batch:
                print(f"[{NAME}] Page {page}/{max_pages}: fetched {len(page_batch)} candidates")
                yield page_batch
            else:
                break
                
            page += 1
        except Exception as e:
            print(f"[{NAME}] error on page {page}: {e}")
            break
