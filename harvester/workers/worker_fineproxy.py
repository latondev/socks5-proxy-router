import re
import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT, FIRECRAWL_API_KEY

NAME = "FineProxy"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Scrapes FineProxy free proxy list (1,000+ candidates).
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    url = "https://fineproxy.org/free-proxy/"
    
    candidates = []
    
    # 1. Try Firecrawl
    if FIRECRAWL_API_KEY:
        try:
            from firecrawl import FirecrawlApp
            fc_app = FirecrawlApp(api_key=FIRECRAWL_API_KEY)
            res = fc_app.scrape(url, formats=["markdown"])
            md = getattr(res, "markdown", "") or ""
            matches = re.findall(r'\b(?P<ip>(?:\d{1,3}\.){3}\d{1,3}):(?P<port>\d{2,5})\b', md)
            for ip, port in matches:
                candidates.append({
                    "ip": ip.strip(),
                    "port": port.strip(),
                    "source": "fineproxy"
                })
        except Exception as e:
            print(f"[{NAME}] Firecrawl fetch error: {e}")
            
    # 2. Fallback to direct HTTP
    if not candidates:
        try:
            r = requests.get(url, headers=headers, timeout=12)
            if r.status_code == 200:
                matches = re.findall(r'\b(?P<ip>(?:\d{1,3}\.){3}\d{1,3}):(?P<port>\d{2,5})\b', r.text)
                for ip, port in matches:
                    candidates.append({
                        "ip": ip.strip(),
                        "port": port.strip(),
                        "source": "fineproxy"
                    })
        except Exception as e:
            print(f"[{NAME}] direct fetch error: {e}")
            
    if candidates:
        print(f"[{NAME}] extracted {len(candidates)} candidates")
        # Yield in batches of 100
        for i in range(0, len(candidates), 100):
            yield candidates[i:i+100]
