import os

ROUTER_FEED_URL = os.getenv("ROUTER_FEED_URL", "http://localhost:8080/api/feed")
ROUTER_AUTH_USER = os.getenv("ROUTER_AUTH_USER", os.getenv("SOCKS_USER", ""))
ROUTER_AUTH_PASS = os.getenv("ROUTER_AUTH_PASS", os.getenv("SOCKS_PASS", ""))
FIRECRAWL_API_KEY = os.getenv("FIRECRAWL_API_KEY", "fc-ca86eb307fe74f40aa5177f572693565")
CHECK_TIMEOUT = float(os.getenv("CHECK_TIMEOUT", "2.5"))
CYCLE_INTERVAL = int(os.getenv("CYCLE_INTERVAL", "600"))  # 10 minutes between full cycles
CONCURRENT_CHECKS = int(os.getenv("CONCURRENT_CHECKS", "35"))
BATCH_FEED_SIZE = int(os.getenv("BATCH_FEED_SIZE", "20"))
DEFAULT_USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

