@echo off
REM Launch FlowLite from source (Windows).
cd /d "%~dp0"

if not exist .venv (
  echo Setting up for the first time...
  where uv >nul 2>nul
  if %errorlevel%==0 (
    uv venv --python 3.12 && uv pip install -e .
  ) else (
    python -m venv .venv && .venv\Scripts\pip install -e .
  )
)

start "" .venv\Scripts\pythonw.exe -m flowlite %*
