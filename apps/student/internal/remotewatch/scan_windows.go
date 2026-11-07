//go:build windows

package remotewatch

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// smRemoteSession is the system metric that is not zero while the process runs in a Remote Desktop session.
const smRemoteSession = 0x1000

// wtsClientName is the information class that names the computer a Remote Desktop session is connected from.
const wtsClientName = 10

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	wtsapi32                 = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInfoW = wtsapi32.NewProc("WTSQuerySessionInformationW")
)

// processNames are the names of the running programs (the executable files, as in the task manager), of every session.
func processNames() ([]string, bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, false
	}
	var names []string
	for {
		if name := windows.UTF16ToString(entry.ExeFile[:]); name != "" {
			names = append(names, name)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return names, true
}

// remoteSession says whether the student's session is a Remote Desktop one: the system says so for a session that someone
// connected to (a takeover of the session on the screen, as much as a session that began remote), whatever program they used.
// The detail is the name of the computer it comes from, when the system gives it.
func remoteSession() (Finding, bool) {
	remote, _, _ := procGetSystemMetrics.Call(smRemoteSession)
	if remote == 0 {
		return Finding{}, false
	}
	return Finding{Kind: KindSession, Name: "Remote Desktop", Detail: truncate(clientName(), maxDetail)}, true
}

// clientName is the name of the computer that the session is connected from; empty when it cannot be read.
func clientName() string {
	var own uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &own); err != nil {
		return ""
	}
	var buffer *uint16
	var size uint32
	// A zero server handle is this computer. The call puts a pointer to the name into buffer, which must be freed.
	ok, _, _ := procWTSQuerySessionInfoW.Call(0, uintptr(own), wtsClientName, uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 || buffer == nil {
		return ""
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(buffer)))
	return windows.UTF16PtrToString(buffer)
}
