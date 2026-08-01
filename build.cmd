@echo off
setlocal EnableExtensions DisableDelayedExpansion
cd /d "%~dp0"
chcp 65001 >nul
title PCController project-owned build and packaging

where.exe node.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Node.js 20.19 or newer was not found in PATH.
    echo         Install Node.js, then run this command again.
    exit /b 1
)

node.exe "%~dp0Tools\Build\build.mjs" %*
set "RESULT=%ERRORLEVEL%"
if not "%RESULT%"=="0" (
    echo.
    echo [ERROR] PCController build exited with code %RESULT%.
)
exit /b %RESULT%
