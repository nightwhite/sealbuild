package main

import "strings"

var windowsSystemDLLs = map[string]struct{}{
	"advapi32.dll": {}, "bcrypt.dll": {}, "bcryptprimitives.dll": {},
	"cfgmgr32.dll": {}, "combase.dll": {}, "comdlg32.dll": {}, "crypt32.dll": {},
	"cryptbase.dll": {}, "cabinet.dll": {}, "dbghelp.dll": {}, "dhcpcsvc.dll": {},
	"dnsapi.dll": {}, "dwmapi.dll": {}, "dwrite.dll": {}, "dxgi.dll": {}, "fwpuclnt.dll": {}, "gdi32.dll": {},
	"imm32.dll": {}, "iphlpapi.dll": {}, "kernel32.dll": {}, "msvcrt.dll": {},
	"kernelbase.dll": {}, "hid.dll": {}, "imagehlp.dll": {}, "mpr.dll": {}, "mswsock.dll": {},
	"ncrypt.dll": {}, "netapi32.dll": {}, "normaliz.dll": {}, "ntdll.dll": {}, "ole32.dll": {},
	"oleaut32.dll": {}, "powrprof.dll": {}, "psapi.dll": {}, "rpcrt4.dll": {},
	"oleacc.dll": {}, "propsys.dll": {}, "rasapi32.dll": {}, "secur32.dll": {}, "sensapi.dll": {},
	"setupapi.dll": {}, "shell32.dll": {}, "shlwapi.dll": {}, "sspcli.dll": {},
	"ucrtbase.dll": {}, "user32.dll": {}, "userenv.dll": {}, "version.dll": {},
	"winhttp.dll": {}, "winmm.dll": {}, "winspool.drv": {}, "wintrust.dll": {},
	"wldap32.dll": {}, "ws2_32.dll": {}, "wsock32.dll": {}, "wtsapi32.dll": {},
}

func isWindowsSystemDLL(name string) bool {
	normalized := strings.ToLower(name)
	if strings.HasPrefix(normalized, "api-ms-win-") || strings.HasPrefix(normalized, "ext-ms-win-") {
		return strings.HasSuffix(normalized, ".dll")
	}
	_, exists := windowsSystemDLLs[normalized]
	return exists
}
