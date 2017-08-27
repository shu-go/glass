set NAME=glass

set GOOS=windows
set GOARCH=386
set ZIPNAME=%NAME%_%GOOS%_%GOARCH%.zip 
go build -ldflags "-s -w" %*
del %ZIPNAME% 2>nul
zip %ZIPNAME% *.exe README.txt LICENSE.txt サンプル*.bat

set GOOS=windows
set GOARCH=amd64
set ZIPNAME=%NAME%_%GOOS%_%GOARCH%.zip 
go build -ldflags "-s -w" %*
del %ZIPNAME% 2>nul
zip %ZIPNAME% *.exe README.txt LICENSE.txt サンプル*.bat
