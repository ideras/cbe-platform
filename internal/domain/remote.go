package domain

import "slices"

// RemoteKindSession and RemoteKindTool say what a RemoteFinding is: a Remote Desktop session, or a remote-control program running.
const (
	RemoteKindSession = "remote_session"
	RemoteKindTool    = "remote_tool"
)

// RemoteFinding is something the student application found that says the exam computer is watched or controlled from elsewhere:
// the kind, the name of the session or program, and more when the application has it (the computer a session comes from). It is
// what the application says, so it is shown as plain text.
type RemoteFinding struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// defaultRemoteTools are the programs the student application looks for while an exam runs: remote-control and screen-sharing
// software, and the clients people use to reach another computer. An entry is a program name without ".exe" and without regard to
// case, or the start of one when it ends with "*". It is a list of names, so a renamed program is not found, and it is not
// meant to be complete: it is what a student is likely to have at hand.
var defaultRemoteTools = []string{
	// Remote support and remote access.
	"teamviewer*", "anydesk*", "rustdesk*", "ultraviewer*", "todesk*", "sunloginclient*", "awesun*", "splashtop*", "srserver",
	"logmein*", "lmiguardian", "screenconnect*", "connectwise*", "dwagent", "radmin*", "rutserv", "supremo*", "ammyy*", "remotepc*",
	"parsec*", "remoting_host", "zohoassist*",
	// What Windows brings: Quick Assist, Remote Assistance and the Remote Desktop client.
	"quickassist", "msra", "mstsc",
	// VNC servers and viewers, and the desktop sharing of Linux and macOS.
	"winvnc*", "tvnserver", "tightvnc*", "ultravnc*", "vncviewer", "vncserver*", "x11vnc", "xvnc*", "krfb", "screensharingd",
}

// DefaultRemoteTools is a copy of the program names the student application looks for.
func DefaultRemoteTools() []string { return slices.Clone(defaultRemoteTools) }
