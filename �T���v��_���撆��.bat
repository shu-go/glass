@echo off
set DEFAULT=youtube �j�R�j�R �r�f�I

set /p TGT=�Ώ�(�ȗ����́A%DEFAULT%): 
if "%TGT%" == "" (
    set TGT=%DEFAULT%
)

glass.exe watch %TGT% -a 30 -i 100ms %*

@echo on
exit /b 0
