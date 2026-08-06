@echo off
setlocal EnableExtensions
cd /d "%~dp0"
chcp 65001 >nul
title AVR firmware studio

where node >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Node.js 22.12 or newer was not found in PATH.
    echo         Install Node.js, then run this command again.
    exit /b 1
)

for /f "usebackq delims=" %%I in (`node -p "require('./Tools/Controller/web/package.json').productName"`) do set "PRODUCT_NAME=%%I"
if not defined PRODUCT_NAME set "PRODUCT_NAME=Controller"
title %PRODUCT_NAME% AVR firmware studio

node "%~dp0Tools\Firmware\firmware.mjs" %*
set "RESULT=%ERRORLEVEL%"
exit /b %RESULT%
