@echo off
set DEFAULT=youtube ニコニコ ビデオ

set /p TGT=対象(省略時は、%DEFAULT%): 
if "%TGT%" == "" (
    set TGT=%DEFAULT%
)

glass.exe watch %TGT% -a 30 -i 100ms %*

@echo on
exit /b 0
