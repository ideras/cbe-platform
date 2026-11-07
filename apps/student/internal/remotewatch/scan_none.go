//go:build !windows

package remotewatch

// remoteSession says whether the session is a Remote Desktop one. Only Windows has a Remote Desktop session to recognise here;
// other systems are looked at through the programs they run.
func remoteSession() (Finding, bool) { return Finding{}, false }
