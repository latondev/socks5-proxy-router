import socket
import ssl
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import List, Dict, Any, Optional

def test_socks5_google(ip: str, port: int, timeout: float = 2.5) -> Optional[int]:
    """
    Tests SOCKS5 connectivity and real Google TLS 443 handshake.
    Returns latency in milliseconds if successful, None otherwise.
    """
    start_time = time.time()
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.settimeout(timeout)
    
    try:
        s.connect((ip, int(port)))
        
        # 1. SOCKS5 greeting: VER=5, NMETHODS=1, METHOD=0 (No Auth)
        s.sendall(b"\x05\x01\x00")
        resp = s.recv(2)
        if len(resp) < 2 or resp[0] != 0x05 or resp[1] != 0x00:
            s.close()
            return None
            
        # 2. SOCKS5 connect to www.google.com:443
        target = b"www.google.com"
        req = b"\x05\x01\x00\x03" + bytes([len(target)]) + target + (443).to_bytes(2, "big")
        s.sendall(req)
        
        conn_resp = s.recv(256)
        if len(conn_resp) < 2 or conn_resp[1] != 0x00:
            s.close()
            return None
            
        # 3. Direct TLS handshake with Google
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        
        tls_sock = context.wrap_socket(s, server_hostname="www.google.com")
        tls_sock.settimeout(timeout)
        tls_sock.do_handshake()
        tls_sock.close()
        
        latency_ms = int((time.time() - start_time) * 1000)
        return latency_ms
    except Exception:
        try:
            s.close()
        except Exception:
            pass
        return None

def check_batch_concurrent(proxies: List[Dict[str, Any]], max_workers: int = 30, timeout: float = 2.5) -> List[Dict[str, Any]]:
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
            
        lat = test_socks5_google(ip, port_num, timeout=timeout)
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
