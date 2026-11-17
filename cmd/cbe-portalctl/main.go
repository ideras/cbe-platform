// cbe-portalctl configures a CBE Portal server: the accounts, the folders, the XFS volume of the workspace exams and the services, from a
// configuration file or from a few questions. The installer puts it in /opt/cbe-portal/bin; docs/deployment-linux.md says how it is used.
package main

import (
	"os"

	"exam-platform/internal/portalctl"
)

// version is stamped into the program by the build (-X main.version=v0.17.0).
var version = "dev"

func main() {
	os.Exit(portalctl.Main(os.Args[1:], portalctl.RealEnv(version)))
}
