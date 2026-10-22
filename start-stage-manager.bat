@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0stage_mgr_go"

set "OUT=build\windows-amd64\Stage Manager for Windows.exe"
set "TASKLIST=%SystemRoot%\System32\tasklist.exe"
set "FIND=%SystemRoot%\System32\find.exe"

rem 已经有一个在跑：先让它退出，好把停放的窗口都放回桌面。
"%TASKLIST%" /fi "imagename eq Stage Manager for Windows.exe" 2>nul | "%FIND%" /i "Stage Manager for Windows.exe" >nul
if not errorlevel 1 (
    echo 正在退出已运行的 Stage Manager for Windows...
    if exist "%OUT%" "%OUT%" --quit
    timeout /t 2 >nul 2>&1
)

echo 正在编译 Stage Manager for Windows...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "%OUT%" .
if errorlevel 1 (
    echo.
    echo 编译失败，请检查上面的错误信息。
    pause
    exit /b 1
)

start "" "%OUT%"
echo Stage Manager for Windows 已启动。
timeout /t 2 >nul 2>&1
exit /b 0
