@echo off
setlocal EnableExtensions DisableDelayedExpansion
if not exist "%~dp0Tools\Build\node_modules\chalk\package.json" goto :install_build_dependencies
if not exist "%~dp0Tools\Build\node_modules\cli-table3\package.json" goto :install_build_dependencies
goto :run_update

:install_build_dependencies
node "%~dp0Tools\Build\env-bootstrap.mjs" install-build-dependencies
if errorlevel 1 exit /b 1

:run_update
node "%~dp0Tools\Dependencies\update.mjs" %*
exit /b %errorlevel%
