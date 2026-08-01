@echo off
setlocal EnableExtensions
cd /d "%~dp0"
chcp 65001 >nul
title PCController AVR firmware studio

where node >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Node.js 20.19 or newer was not found in PATH.
    exit /b 1
)

node "%~dp0Tools\Firmware\firmware.mjs" %*
set "RESULT=%ERRORLEVEL%"
if not "%RESULT%"=="0" (
    echo.
    echo [ERROR] Firmware utility exited with code %RESULT%.
)
exit /b %RESULT%
