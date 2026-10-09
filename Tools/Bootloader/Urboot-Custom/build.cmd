@echo off
setlocal
node "%~dp0build.mjs" %*
exit /b %errorlevel%
