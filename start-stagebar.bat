@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set "EXE=StageManager\bin\Release\net8.0-windows10.0.19041.0\StageBar.exe"

rem Use the system tools explicitly; a PATH with Git or MSYS tools first
rem would otherwise pick up their unrelated "find".
"%SystemRoot%\System32\tasklist.exe" /fi "imagename eq StageBar.exe" 2>nul | "%SystemRoot%\System32\find.exe" /i "StageBar.exe" >nul
if not errorlevel 1 (
    echo StageBar 已在运行。如需重启，请先在托盘图标菜单中选择“退出”。
    timeout /t 3 >nul 2>&1
    exit /b 0
)

echo 正在编译 StageBar...
dotnet build "StageManager\StageManager.csproj" -c Release --nologo -v quiet
if errorlevel 1 (
    echo 编译失败，请检查上面的错误信息。
    pause
    exit /b 1
)

start "" "%EXE%"
echo StageBar 已启动。
timeout /t 2 >nul 2>&1
exit /b 0
