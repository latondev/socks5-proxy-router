import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "ProxyScrape"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Paginates through ProxyScrape official v4 API page by page (100+ pages, 50 proxies/page).
    Endpoint: https://api.proxyscrape.com/v4/free-proxy-list/get
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    limit = 50
    page = 1
    max_pages = 100  # Crawl up to 100 pages per cycle (~5,000 proxies)
    skip = 0
    
    while page <= max_pages:
        url = f"https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&protocol=socks5&proxy_format=protocolipport&format=json&limit={limit}&skip={skip}"
        try:
            r = requests.get(url, headers=headers, timeout=12)
            if r.status_code != 200:
                break
                
            data = r.json()
            proxies_data = data.get("proxies", [])
            total_records = data.get("total_records", 5000)
            total_calc_pages = min(max_pages, (total_records + limit - 1) // limit)
            
            if not proxies_data:
                break
                
            page_batch = []
            for item in proxies_data:
                ip = item.get("ip")
                port = item.get("port")
                if not ip or not port:
                    continue
                
                ip_data = item.get("ip_data", {})
                page_batch.append({
                    "ip": str(ip).strip(),
                    "port": str(port).strip(),
                    "country": ip_data.get("country", ""),
                    "country_code": ip_data.get("countryCode", ""),
                    "city": ip_data.get("city", ""),
                    "source": "proxyscrape",
                    "total_pages": total_calc_pages,
                    "page": page
                })
                
            if page_batch:
                print(f"[{NAME}] Page {page}/{total_calc_pages}: fetched {len(page_batch)} candidates (skip={skip})")
                yield page_batch
                
            if len(proxies_data) < limit:
                break
                
            skip += limit
            page += 1
        except Exception as e:
            print(f"[{NAME}] error on page {page}: {e}")
            break
