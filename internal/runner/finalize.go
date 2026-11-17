package runner

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

// FinalizeFile is a file of the build context of the last layer.
type FinalizeFile struct {
	Name string
	Mode int64
	Data []byte
}

// AgentSum is the SHA-256 of an agent, as the image's label has it.
func AgentSum(agent []byte) string {
	sum := sha256.Sum256(agent)
	return hex.EncodeToString(sum[:])
}

// FinalizeFiles are the files of the build context of the last layer: its Containerfile, the script and the agent. The runner builds them on top of the
// author's image; `cbe-portalctl image check` builds the same files on an author's computer, so that what it says is what the runner will say.
func FinalizeFiles(base string, agent []byte) []FinalizeFile {
	return finalizeFiles(base, agent, AgentSum(agent))
}

func finalizeFiles(base string, agent []byte, agentSum string) []FinalizeFile {
	return []FinalizeFile{
		{"Containerfile", 0o644, []byte(finalizeContainerfile(base, agentSum))},
		{"cbe-finalize.sh", 0o755, finalizeScript},
		{"cbe-agent", 0o755, agent},
	}
}

// finalizeContext is the build context of the last layer as a tar archive.
func finalizeContext(base string, agent []byte, agentSum string) (io.Reader, error) {
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, file := range finalizeFiles(base, agent, agentSum) {
		if err := w.WriteHeader(&tar.Header{Name: file.Name, Typeflag: tar.TypeReg, Mode: file.Mode, Size: int64(len(file.Data)), ModTime: time.Unix(0, 0)}); err != nil {
			return nil, err
		}
		if _, err := w.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &out, nil
}
