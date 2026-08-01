@echo off
setlocal EnableExtensions DisableDelayedExpansion
if not exist "%~dp0Tools\Build\node_modules\chalk\package.json" goto :install_build_dependencies
if not exist "%~dp0Tools\Build\node_modules\cli-table3\package.json" goto :install_build_dependencies
goto :run_update

:install_build_dependencies
where.exe npm.cmd >nul 2>nul
if errorlevel 1 (
    echo [ERROR] npm was not found; it is required for the locked terminal presentation dependencies.
    exit /b 1
)
call npm.cmd --prefix "%~dp0Tools\Build" ci --ignore-scripts --no-audit --no-fund
if errorlevel 1 exit /b 1

:run_update
node "%~dp0Tools\Dependencies\update.mjs" %*
exit /b %errorlevel%
