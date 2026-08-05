@echo off
rem 编译 fastKey（使用本目录下的便携版 Go）
set "PATH=%~dp0go\bin;%PATH%"
go build -ldflags="-H windowsgui" -o fastKey.exe .
go build -o fastKey-test.exe .
echo 编译完成: fastKey.exe (正式版) / fastKey-test.exe (测试版)
pause
