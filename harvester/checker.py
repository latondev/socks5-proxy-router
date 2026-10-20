import re
import time
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import List, Dict, Any, Optional

IP_REGEX = re.compile(r"^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$")

def test_socks5_live(ip: str, port: int, timeout: float = 5.0) -> Optional[int]:
    """
    Tests SOCKS5 connectivity against api.ipify.org and www.youtube.com/generate_204.
    Returns latency in milliseconds if both succeed, None otherwise.
    """
    proxy_url = f"socks5h://{ip}:{port}"
    proxies = {"http": proxy_url, "https": proxy_url}
    start = time.time()
    session = requests.Session()
    session.headers.update({"User-Agent": "curl/8.4.0"})
    try:
        # 1. Verify outbound IPv4 & DNS via api.ipify.org
        r1 = session.get("https://api.ipify.org", proxies=proxies, timeout=timeout)
        if r1.status_code != 200:
            return None
        egress = r1.text.strip()
        if not IP_REGEX.match(egress):
            return None

        # 2. Verify YouTube is reachable (returns 204 No Content)
        r2 = session.get("https://www.youtube.com/generate_204", proxies=proxies, timeout=timeout)
        if r2.status_code < 200 or r2.status_code >= 400:
            return None

        latency_ms = int((time.time() - start) * 1000)
        return latency_ms
    except Exception:
        return None
    finally:
        session.close()

def check_batch_concurrent(proxies: List[Dict[str, Any]], max_workers: int = 30, timeout: float = 5.0) -> List[Dict[str, Any]]:
    """
    Checks a batch of proxy dictionaries concurrently.
    Returns list of only alive proxies with measured 'latency_ms'.
    """
    alive = []
    
    def worker(p: Dict[str, Any]):
        ip = p.get("ip")
        port = p.get("port")
        if not ip or not port:
            return None
        try:
            port_num = int(port)
        except ValueError:
            return None
            
        lat = test_socks5_live(ip, port_num, timeout=timeout)
        if lat is not None:
            res = dict(p)
            res["latency_ms"] = lat
            return res
        return None

    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = [executor.submit(worker, p) for p in proxies]
        for f in as_completed(futures):
            res = f.result()
            if res:
                alive.append(res)
                
    return alive
