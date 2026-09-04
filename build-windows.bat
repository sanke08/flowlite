@echo off
REM Build the Windows release into app\v<version>\.
setlocal enabledelayedexpansion
cd /d "%~dp0"

for /f "tokens=2 delims==" %%a in ('findstr /b "__version__" flowlite\__init__.py') do (
  set VERSION=%%a
)
set VERSION=%VERSION: =%
set VERSION=%VERSION:"=%

set OUT=app\v%VERSION%
set ZIP=%OUT%\FlowLite-%VERSION%-Windows-x64.zip

if not exist .venv (
  echo No .venv - run run.bat once first to set up.
  exit /b 1
)

echo ==^> FlowLite %VERSION% ^(Windows x64^)
if exist build rmdir /s /q build
if exist dist rmdir /s /q dist
.venv\Scripts\pyinstaller.exe FlowLite.spec --noconfirm
if errorlevel 1 exit /b 1

echo ==^> Packaging ZIP
if not exist "%OUT%" mkdir "%OUT%"
if exist "%ZIP%" del "%ZIP%"
powershell -NoProfile -Command ^
  "Compress-Archive -Path 'dist\FlowLite\*' -DestinationPath '%ZIP%' -Force"

powershell -NoProfile -Command ^
  "(Get-FileHash '%ZIP%' -Algorithm SHA256).Hash.ToLower() + '  ' + (Split-Path '%ZIP%' -Leaf) | Set-Content '%ZIP%.sha256'"

echo.
echo Release artifact:
echo   %ZIP%
type "%ZIP%.sha256"
