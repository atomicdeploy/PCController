@echo off
setlocal EnableExtensions DisableDelayedExpansion
cd /d "%~dp0"
chcp 65001 >nul
title Project-owned build and packaging

where.exe node.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Node.js 22.12 or newer was not found in PATH.
    echo         Install Node.js, then run this command again.
    exit /b 1
)

for /f "usebackq delims=" %%I in (`node.exe "%~dp0Tools\Build\env-bootstrap.mjs" title`) do set "PRODUCT_NAME=%%I"
if not defined PRODUCT_NAME set "PRODUCT_NAME=Controller"
title "%PRODUCT_NAME% project-owned build and packaging"

if not exist "%~dp0Tools\Build\node_modules\chalk\package.json" goto :install_build_dependencies
if not exist "%~dp0Tools\Build\node_modules\cli-table3\package.json" goto :install_build_dependencies
goto :run_build

:install_build_dependencies
echo [SETUP] Installing locked build UI dependencies...
node.exe "%~dp0Tools\Build\env-bootstrap.mjs" install-build-dependencies
if errorlevel 1 exit /b 1

:run_build
node.exe "%~dp0Tools\Build\build.mjs" %*
set "RESULT=%ERRORLEVEL%"
exit /b %RESULT%
