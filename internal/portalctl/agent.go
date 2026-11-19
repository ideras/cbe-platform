package portalctl

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// agentFor finds the agent an image is built with (decision D-CB-9): the file named with --agent; else the server's own (agent_binary in the runner's
// settings, serverAgent), when a server is installed here; else the cbe-agent next to this program, which after `make agent portalctl` is build/cbe-agent
// beside build/cbe-portalctl. PATH is not searched: the agent is the first process of every student's container, and one that happened to be first on
// PATH is not one anybody chose. settings is where the server's settings are, or would be, for the error.
func agentFor(env Env, named, serverAgent, settings string) (file, from string, err error) {
	switch {
	case named != "":
		return named, "named with --agent", nil
	case serverAgent != "":
		return serverAgent, "the server's: agent_binary in " + settings, nil
	}
	next := filepath.Join(filepath.Dir(env.Exe), "cbe-agent")
	if fileExists(next) {
		return next, "the one next to this program", nil
	}
	return "", "", fmt.Errorf("there is no agent to put in the image: --agent names none, no server is installed here (%s says none), and there is no %s. "+
		"Build one with `make agent` (it goes in build/, next to build/cbe-portalctl), or name one with --agent FILE", settings, next)
}

// releaseHere is the release the agent should be: the one of the server installed here (INSTALL_DIR/VERSION), or this program's own.
func releaseHere(env Env, install string) (release, of string) {
	if data, err := env.Sys.ReadFile(path.Join(install, "VERSION")); err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data)), "the server installed here"
	}
	return env.Version, "this cbe-portalctl"
}

// epochFromEnvironment is the date the images are stamped with when SOURCE_DATE_EPOCH says one (seconds since 1970), or 0 for the default.
func epochFromEnvironment() int64 {
	if n, err := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}
