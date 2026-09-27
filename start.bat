@echo off
title SOCKS5 Fast Proxy Pool
cd /d "%~dp0"
echo ========================================================
echo   SOCKS5 Fast Proxy Pool
echo   - SOCKS5 Endpoint : 127.0.0.1:1080
echo   - Web Dashboard   : http://localhost:8080
echo ========================================================
echo.
socks5-pool.exe
pause
