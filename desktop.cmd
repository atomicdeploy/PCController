@echo off
setlocal EnableExtensions DisableDelayedExpansion
where.exe node.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Node.js was not found in PATH.
    exit /b 1
)
node.exe "%~dp0Tools\Build\run-desktop.mjs" %*
exit /b %ERRORLEVEL%
