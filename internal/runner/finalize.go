package runner

import (
	"archive/tar"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"time"
)

// finalizeScript is the script the last layer of an image with a base of its own runs (finalize/cbe-finalize.sh).
//
//go:embed finalize/cbe-finalize.sh
var finalizeScript []byte

// finalizeVersion changes when what finalizing does changes: it is part of the name of an image, so images made by an older script are made again.
const finalizeVersion = "1"

// finalizeContainerfile is the Containerfile of the last layer: the author's image (base), what finalize.sh does to its files, the agent, the labels and
// the way the image is started. The author cannot reorder it or add to it: it is the runner's, and it comes after everything the author wrote.
func finalizeContainerfile(base, agentSum string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", base)
	b.WriteString("USER root\n")
	b.WriteString("COPY cbe-finalize.sh /cbe-finalize.sh\n")
	b.WriteString("RUN /bin/sh /cbe-finalize.sh && rm -f /cbe-finalize.sh\n")
	b.WriteString("COPY --chmod=0755 cbe-agent " + AgentPath + "\n")
	fmt.Fprintf(&b, "LABEL %s=%q io.cbe.finalized=%q\n", LabelAgent, agentSum, finalizeVersion)
	b.WriteString("HEALTHCHECK NONE\n")
	b.WriteString("USER 1000:1000\n")
	b.WriteString("WORKDIR /work\n")
	b.WriteString("ENTRYPOINT [\"" + AgentPath + "\"]\n")
	b.WriteString("CMD []\n")
	return b.String()
}

// finalizeContext is the build context of the last layer: its Containerfile, the script and the agent.
func finalizeContext(base string, agent []byte, agentSum string) (io.Reader, error) {
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, file := range []struct {
		name string
		mode int64
		data []byte
	}{
		{"Containerfile", 0o644, []byte(finalizeContainerfile(base, agentSum))},
		{"cbe-finalize.sh", 0o755, finalizeScript},
		{"cbe-agent", 0o755, agent},
	} {
		if err := w.WriteHeader(&tar.Header{Name: file.name, Typeflag: tar.TypeReg, Mode: file.mode, Size: int64(len(file.data)), ModTime: time.Unix(0, 0)}); err != nil {
			return nil, err
		}
		if _, err := w.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &out, nil
}
