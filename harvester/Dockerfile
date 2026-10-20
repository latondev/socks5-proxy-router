FROM python:3.12-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . ./harvester

ENV PYTHONUNBUFFERED=1
ENV ROUTER_FEED_URL=http://proxy-router:8080/api/feed

CMD ["python", "-m", "harvester.main"]
