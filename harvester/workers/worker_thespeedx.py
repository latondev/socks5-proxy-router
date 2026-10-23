import re
import requests
from typing import Generator, List, Dict, Any
from ..config import DEFAULT_USER_AGENT

NAME = "TheSpeedX"

def scrape() -> Generator[List[Dict[str, Any]], None, None]:
    """
    Fetches raw SOCKS5 proxies from TheSpeedX/PROXY-List (updated regularly on GitHub).
    Repo: https://github.com/TheSpeedX/PROXY-List
    """
    headers = {"User-Agent": DEFAULT_USER_AGENT}
    url = "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt"
    
    try:
        r = requests.get(url, headers=headers, timeout=15)
        if r.status_code != 200:
            return
            
        matches = re.findall(r'\b(?P<ip>(?:\d{1,3}\.){3}\d{1,3}):(?P<port>\d{2,5})\b', r.text)
        batch = []
        for ip, port in matches:
            batch.append({
                "ip": ip.strip(),
                "port": port.strip(),
                "source": "thespeedx"
            })
            if len(batch) >= 150:
                yield batch
                batch = []
                
        if batch:
            print(f"[{NAME}] loaded {len(matches)} candidates")
            yield batch
    except Exception as e:
        print(f"[{NAME}] error: {e}")
