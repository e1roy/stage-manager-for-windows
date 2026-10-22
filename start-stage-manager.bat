@echo off
setlocal
cd /d "%~dp0stage_mgr_go"

set "OUT=build\windows-amd64\StageManagerForWindows.exe"
set "TASKLIST=%SystemRoot%\System32\tasklist.exe"
set "FIND=%SystemRoot%\System32\find.exe"

rem An instance is already running: ask it to quit first so parked windows go home.
"%TASKLIST%" /fi "imagename eq StageManagerForWindows.exe" /fo csv 2>nul | "%FIND%" /i "StageManagerForWindows.exe" >nul
if not errorlevel 1 (
    echo Stopping the running Stage Manager for Windows...
    if exist "%OUT%" "%OUT%" --quit
    timeout /t 2 >nul 2>&1
)

rem mygo build embeds the icon, manifest and version (a temporary .syso that
rem plain "go build" never sees) and writes the installer next to the exe.
echo Packaging StageManagerForWindows...
go tool mygo build
if errorlevel 1 (
    echo.
    echo Build failed. See the errors above.
    pause
    exit /b 1
)

echo.
echo Exe:   %CD%\%OUT%
for %%f in ("%OUT%\..\*Setup*.exe") do echo Setup: %%~ff

start "" "%OUT%"
echo Started: %CD%\%OUT%
timeout /t 2 >nul 2>&1
exit /b 0
