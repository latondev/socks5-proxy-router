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
            
        # 2. SOCKS5 connect to cloud.zed.dev:443
        target = b"cloud.zed.dev"
        req = b"\x05\x01\x00\x03" + bytes([len(target)]) + target + (443).to_bytes(2, "big")
        s.sendall(req)
        
        conn_resp = s.recv(256)
        if len(conn_resp) < 2 or conn_resp[1] != 0x00:
            s.close()
            return None
            
        # 3. Direct TLS handshake with valid certificate verification (rejects MITM / self-signed proxies)
        context = ssl.create_default_context()
        tls_sock = context.wrap_socket(s, server_hostname="cloud.zed.dev")
        tls_sock.settimeout(timeout)
        tls_sock.do_handshake()
        
        # 4. HTTP verification to ensure bidirectional data transfer
        http_req = b"GET / HTTP/1.1\r\nHost: cloud.zed.dev\r\nUser-Agent: Zed/0.180.0\r\nConnection: close\r\n\r\n"
        tls_sock.sendall(http_req)
        resp_http = tls_sock.recv(256)
        tls_sock.close()
        
        if not resp_http.startswith(b"HTTP/"):
            return None
            
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
