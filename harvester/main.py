import time
import requests
import sys
from typing import List, Dict, Any

from .config import (
    ROUTER_FEED_URL,
    ROUTER_AUTH_USER,
    ROUTER_AUTH_PASS,
    CHECK_TIMEOUT,
    CYCLE_INTERVAL,
    CONCURRENT_CHECKS,
)
from .checker import check_batch_concurrent

# Import all 9 workers
from .workers import (
    worker_proxyscrape,
    worker_geonode,
    worker_proxora,
    worker_proxy_free,
    worker_freeproxy,
    worker_fineproxy,
    worker_proxify,
    worker_monosans,
    worker_socks5_github,
)

WORKERS = [
    worker_proxyscrape,
    worker_geonode,
    worker_proxora,
    worker_proxy_free,
    worker_freeproxy,
    worker_fineproxy,
    worker_proxify,
    worker_monosans,
    worker_socks5_github,
]

WORKER_META = {
    worker_proxyscrape: {"id": "proxyscrape", "name": "ProxyScrape", "total_pages": 96},
    worker_geonode: {"id": "geonode", "name": "Geonode", "total_pages": 12},
    worker_proxora: {"id": "proxora", "name": "Proxora.io", "total_pages": 5},
    worker_proxy_free: {"id": "proxy_free", "name": "Proxy-Free", "total_pages": 1},
    worker_freeproxy: {"id": "freeproxy", "name": "FreeProxy.World", "total_pages": 15},
    worker_fineproxy: {"id": "fineproxy", "name": "FineProxy", "total_pages": 1},
    worker_proxify: {"id": "proxify", "name": "Proxify.vn", "total_pages": 1},
    worker_monosans: {"id": "monosans", "name": "Monosans Feed", "total_pages": 1},
    worker_socks5_github: {"id": "socks5_github", "name": "socks5-proxy.github.io", "total_pages": 1},
}

STATUS_ENDPOINT = ROUTER_FEED_URL.replace("/api/feed", "/api/harvester/status")

def report_worker_status(worker_id: str, data: Dict[str, Any]):
    """Reports real-time worker runtime telemetry to Router API."""
    try:
        auth = (ROUTER_AUTH_USER, ROUTER_AUTH_PASS) if ROUTER_AUTH_USER and ROUTER_AUTH_PASS else None
        body = {"id": worker_id, **data}
        requests.post(STATUS_ENDPOINT, json=body, auth=auth, timeout=3)
    except Exception:
        pass

def feed_router(proxies: List[Dict[str, Any]]) -> bool:
    """
    POSTs verified alive proxies to the Proxy Router feed endpoint.
    """
    if not proxies:
        return False
        
    try:
        auth = None
        if ROUTER_AUTH_USER and ROUTER_AUTH_PASS:
            auth = (ROUTER_AUTH_USER, ROUTER_AUTH_PASS)
        r = requests.post(ROUTER_FEED_URL, json=proxies, auth=auth, timeout=10)
        if r.status_code == 200:
            res = r.json()
            print(f"[FEED SUCCESS] -> Router pool now has {res.get('total_pool_size')} proxies (+{res.get('added')} added)")
            return True
        else:
            print(f"[FEED ERROR] -> Router returned HTTP {r.status_code}: {r.text}")
    except requests.exceptions.ConnectionError:
        print(f"[FEED WARN] -> Cannot connect to Proxy Router at {ROUTER_FEED_URL}. Is Router running?")
    except Exception as e:
        print(f"[FEED ERROR] -> {e}")
    return False

def run_harvester_cycle():
    print("\n" + "=" * 65)
    print(f"🚀 [HARVESTER] Starting proxy harvesting cycle across {len(WORKERS)} sources")
    print(f"Target Router: {ROUTER_FEED_URL}")
    print("=" * 65)
    
    seen_in_cycle = set()
    total_candidates = 0
    total_verified_alive = 0
    
    for worker in WORKERS:
        meta = WORKER_META.get(worker, {"id": worker.__name__, "name": getattr(worker, "NAME", worker.__name__), "total_pages": 1})
        wid = meta["id"]
        wname = meta["name"]
        w_total_pages = meta["total_pages"]
        
        print(f"\n---> Running Worker: {wname}")
        report_worker_status(wid, {
            "name": wname,
            "status": "running",
            "current_page": 1,
            "total_pages": w_total_pages,
            "current_step": f"Bắt đầu cào dữ liệu từ nguồn {wname}...",
            "recent_logs": [f"[{time.strftime('%H:%M:%S')}] Khởi động crawler {wname}"]
        })
        
        w_candidates = 0
        w_alive = 0
        w_dead = 0
        w_latencies = []
        logs = []
        page_counter = 0

        try:
            for page_batch in worker.scrape():
                page_counter += 1
                if page_batch and "total_pages" in page_batch[0]:
                    w_total_pages = page_batch[0]["total_pages"]
                curr_page = page_batch[0].get("page", page_counter) if page_batch else page_counter
                t_str = time.strftime('%H:%M:%S')

                # 1. Deduplicate in memory
                new_candidates = []
                for p in page_batch:
                    addr = f"{p.get('ip')}:{p.get('port')}"
                    if addr not in seen_in_cycle:
                        seen_in_cycle.add(addr)
                        new_candidates.append(p)
                        
                total_candidates += len(new_candidates)
                w_candidates += len(new_candidates)
                
                log_entry = f"[{t_str}] [Page {curr_page}/{w_total_pages}] Lấy về {len(page_batch)} IP, sau khử trùng còn {len(new_candidates)} IP"
                logs.append(log_entry)
                print(log_entry)

                if not new_candidates:
                    report_worker_status(wid, {
                        "status": "running",
                        "current_page": curr_page,
                        "total_pages": w_total_pages,
                        "current_step": f"Trang {curr_page}: Không có IP mới sau deduplication",
                        "candidates": w_candidates,
                        "alive": w_alive,
                        "dead": w_dead,
                        "recent_logs": logs[-4:]
                    })
                    continue
                    
                # 2. Concurrently verify connectivity & Google TLS handshake
                report_worker_status(wid, {
                    "status": "running",
                    "current_page": curr_page,
                    "total_pages": w_total_pages,
                    "current_step": f"Đang chạy 35 luồng test Google TLS 443 cho {len(new_candidates)} IP (Trang {curr_page})...",
                    "candidates": w_candidates,
                    "alive": w_alive,
                    "dead": w_dead,
                    "recent_logs": logs[-4:]
                })
                
                alive = check_batch_concurrent(
                    new_candidates,
                    max_workers=CONCURRENT_CHECKS,
                    timeout=CHECK_TIMEOUT
                )
                
                batch_dead = len(new_candidates) - len(alive)
                w_dead += batch_dead
                
                # 3. Stream alive batch to Proxy Router immediately!
                if alive:
                    total_verified_alive += len(alive)
                    w_alive += len(alive)
                    batch_lats = [p.get("latency_ms", 250) for p in alive]
                    w_latencies.extend(batch_lats)
                    avg_lat = int(sum(w_latencies) / len(w_latencies)) if w_latencies else 250
                    
                    stream_log = f"[{time.strftime('%H:%M:%S')}] ✅ {len(alive)}/{len(new_candidates)} ALIVE! (avg {avg_lat}ms) -> Đã stream sang Router pool"
                    logs.append(stream_log)
                    print(f"[{wname}] {stream_log}")
                    feed_router(alive)
                    
                    report_worker_status(wid, {
                        "status": "running",
                        "current_page": curr_page,
                        "total_pages": w_total_pages,
                        "current_step": f"Đã stream {len(alive)} proxy sống sang Router pool (Trang {curr_page})",
                        "candidates": w_candidates,
                        "alive": w_alive,
                        "dead": w_dead,
                        "avg_latency_ms": avg_lat,
                        "recent_logs": logs[-4:]
                    })
                else:
                    fail_log = f"[{time.strftime('%H:%M:%S')}] ❌ 0/{len(new_candidates)} proxies alive in this batch."
                    logs.append(fail_log)
                    print(f"[{wname}] {fail_log}")
                    report_worker_status(wid, {
                        "status": "running",
                        "current_page": curr_page,
                        "total_pages": w_total_pages,
                        "current_step": f"Trang {curr_page}: 0/{len(new_candidates)} proxy vượt qua TLS test",
                        "candidates": w_candidates,
                        "alive": w_alive,
                        "dead": w_dead,
                        "recent_logs": logs[-4:]
                    })
            
            # Worker loop ended
            avg_lat = int(sum(w_latencies) / len(w_latencies)) if w_latencies else 0
            final_log = f"[{time.strftime('%H:%M:%S')}] 🏁 Hoàn tất: {w_alive} ALIVE / {w_candidates} Candidates"
            logs.append(final_log)
            report_worker_status(wid, {
                "status": "completed",
                "current_page": w_total_pages,
                "total_pages": w_total_pages,
                "current_step": f"Hoàn thành cào: {w_alive} proxy sống / {w_candidates} candidates",
                "candidates": w_candidates,
                "alive": w_alive,
                "dead": w_dead,
                "avg_latency_ms": avg_lat,
                "recent_logs": logs[-4:]
            })
        except Exception as e:
            err_log = f"[{time.strftime('%H:%M:%S')}] Lỗi: {e}"
            logs.append(err_log)
            print(f"[{wname}] Worker encountered error: {e}")
            report_worker_status(wid, {
                "status": "error",
                "current_step": f"Lỗi crawler: {e}",
                "recent_logs": logs[-4:]
            })
            
    print("\n" + "=" * 65)
    print(f"🏁 [CYCLE COMPLETE] Total Candidates: {total_candidates} | Verified Alive: {total_verified_alive}")
    print(f"Next cycle in {CYCLE_INTERVAL} seconds ({CYCLE_INTERVAL // 60} minutes)...")
    print("=" * 65)


def main():
    print("=================================================================")
    print("   SOCKS5 Multi-Source Proxy Harvester & Streaming Engine")
    print(f"   - Target Router  : {ROUTER_FEED_URL}")
    print(f"   - Cycle Interval : {CYCLE_INTERVAL}s")
    print("=================================================================")
    
    while True:
        try:
            run_harvester_cycle()
        except KeyboardInterrupt:
            print("\nHarvester stopped by user.")
            sys.exit(0)
        except Exception as e:
            print(f"\n[UNEXPECTED ERROR in cycle]: {e}")
            
        time.sleep(CYCLE_INTERVAL)

if __name__ == "__main__":
    main()
