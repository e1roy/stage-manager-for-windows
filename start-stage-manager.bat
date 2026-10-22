@echo off
setlocal
cd /d "%~dp0stage_mgr_go"

set "OUT=build\windows-amd64\Stage Manager for Windows.exe"
set "TASKLIST=%SystemRoot%\System32\tasklist.exe"
set "FIND=%SystemRoot%\System32\find.exe"

rem An instance is already running: ask it to quit first so parked windows go home.
"%TASKLIST%" /fi "imagename eq Stage Manager for Windows.exe" /fo csv 2>nul | "%FIND%" /i "Stage Manager for Windows.exe" >nul
if not errorlevel 1 (
    echo Stopping the running Stage Manager for Windows...
    if exist "%OUT%" "%OUT%" --quit
    timeout /t 2 >nul 2>&1
)

echo Building Stage Manager for Windows...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "%OUT%" .
if errorlevel 1 (
    echo.
    echo Build failed. See the errors above.
    pause
    exit /b 1
)

start "" "%OUT%"
echo Stage Manager for Windows started.
timeout /t 2 >nul 2>&1
exit /b 0
