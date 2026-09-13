@echo off
rem Test fixture for Milestone 1 launcher verification.
rem Validates stdout, stderr, environment variables, arguments, and exit codes.

if "%~1"=="--sleep" goto :do_sleep

if "%~1"=="--exit-code" (
    echo EXITING_WITH_CODE_%~2 1>&2
    exit /b %~2
)

echo DUMMY_BAT_STDOUT: OK

if "%TEST_TRIGGER_STDERR%"=="1" (
    echo DUMMY_BAT_STDERR: NOTICE 1>&2
)

if defined PLUGIN_TEST_ENV (
    echo ENV:%PLUGIN_TEST_ENV%
)

if not "%~1"=="" (
    echo ARGS:%*
)

echo CWD:%CD%
exit /b 0

:do_sleep
set "sleepSec=%~2"
set /a pingCount=sleepSec+1
if not defined pingCount set pingCount=2
if %pingCount% leq 0 set pingCount=1
ping 127.0.0.1 -n %pingCount% >nul
exit /b 0
