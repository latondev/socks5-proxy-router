@echo off
title SOCKS5 Proxy Harvester
cd /d "%~dp0"
echo ========================================================
echo   SOCKS5 Multi-Source Proxy Harvester
echo   Streaming verified proxies to http://localhost:8080/api/feed
echo ========================================================
echo.
python -m harvester.main
pause
