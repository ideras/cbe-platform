package containerfile

import (
	"strings"
	"testing"
)

const digest = "sha256:baac8cde6b9011521f80a878fc8fc475f9cf173da0ee274c2c0d316a9715cbb9"

func TestParse(t *testing.T) {
	text := "# escape=\\\n\n# a comment\nFROM cbe/cpp20 AS base\r\nRUN apt-get update && \\\n    # a comment inside\n    apt-get install -y make=4.3-4.1build2\n\nCOPY --chown=1000:1000 --from=base a b\nRUN <<EOF\necho one\nFROM nothing\nEOF\nUSER 1000:1000\n"
	got, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("%d instructions: %+v", len(got), got)
	}
	if got[0].Command != "FROM" || got[0].Args != "cbe/cpp20 AS base" || got[0].Line != 4 {
		t.Errorf("FROM: %+v", got[0])
	}
	if got[1].Command != "RUN" || got[1].Args != "apt-get update && apt-get install -y make=4.3-4.1build2" || got[1].Line != 5 || got[1].EndLine != 7 {
		t.Errorf("RUN with a continuation: %+v", got[1])
	}
	if got[2].Command != "COPY" || len(got[2].Flags) != 2 || got[2].Args != "a b" {
		t.Errorf("COPY: %+v", got[2])
	}
	if got[3].Command != "RUN" || !strings.Contains(got[3].Args, "FROM nothing") || got[3].EndLine != 13 {
		t.Errorf("the here-document is the RUN's body, not instructions: %+v", got[3])
	}
	if got[4].Command != "USER" || got[4].Line != 14 {
		t.Errorf("USER: %+v", got[4])
	}
	for name, bad := range map[string]string{
		"another escape character":  "# escape=`\nFROM cbe/cpp20\n",
		"a here-document left open": "FROM cbe/cpp20\nRUN <<EOF\necho\n",
		"a null byte":               "FROM cbe/cpp20\x00\n",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := Parse(strings.Repeat("# x\n", MaxBytes/4+10)); err == nil {
		t.Error("a Containerfile over the limit was read")
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name, text string
		problem    string // a text the problems must contain ("" for none)
	}{
		{"the course image alone", "FROM cbe/cpp20\n", ""},
		{"with a digest", "FROM cbe/cpp20@" + digest + "\nUSER root\nRUN echo\nUSER 1000:1000\n", ""},
		{"an ARG before the FROM, a comment, instructions in lower case", "# build\nARG X=1\nfrom cbe/cpp20\nrun echo $X\n", ""},
		{"two stages from the course image and a stage", "FROM cbe/cpp20 AS build\nRUN make\nFROM cbe/cpp20\nCOPY --from=build /a /b\nRUN --mount=type=bind,from=build,source=/a,target=/c true\n", ""},
		{"a stage by number", "FROM cbe/cpp20\nFROM cbe/cpp20\nCOPY --from=0 /a /b\n", ""},
		{"no FROM", "RUN echo\n", "comes before the first FROM"},
		{"an empty file", "\n# nothing\n", "starts with FROM cbe/cpp20"},
		{"another image", "FROM ubuntu:24.04\n", "the only image"},
		{"another course image", "FROM cbe/python-nn\n", "the only image"},
		{"scratch", "FROM scratch\n", "the only image"},
		{"the course image with a tag", "FROM cbe/cpp20:latest\n", "without a tag"},
		{"a bad digest", "FROM cbe/cpp20@sha256:abc\n", "64 hexadecimal"},
		{"a variable", "ARG B=cbe/cpp20\nFROM $B\n", "cannot use a variable"},
		{"a FROM option", "FROM --platform=linux/arm64 cbe/cpp20\n", "takes no options"},
		{"a second stage from another image", "FROM cbe/cpp20\nFROM golang:1.26 AS go\n", "the only image"},
		{"a second stage named like the first image's name", "FROM cbe/cpp20 AS x\nFROM X\n", ""},
		{"a bad stage name", "FROM cbe/cpp20 AS 9x\n", "stage name"},
		{"copy from another image", "FROM cbe/cpp20\nCOPY --from=docker.io/library/golang /go /go\n", "only name an earlier stage"},
		{"a stage that comes later", "FROM cbe/cpp20\nCOPY --from=later /a /b\nFROM cbe/cpp20 AS later\n", "only name an earlier stage"},
		{"a mount from another image", "FROM cbe/cpp20\nRUN --mount=type=bind,from=ubuntu,target=/x true\n", "only name an earlier stage"},
		{"host networking", "FROM cbe/cpp20\nRUN --network=host curl x\n", "host would let the build"},
		{"the build's own network", "FROM cbe/cpp20\nRUN --network=default true\nRUN --network=none true\n", ""},
		{"insecure", "FROM cbe/cpp20\nRUN --security=insecure true\n", "RUN --security"},
		{"a volume", "FROM cbe/cpp20\nVOLUME /etc\n", "VOLUME is not allowed"},
		{"onbuild", "FROM cbe/cpp20\nONBUILD RUN true\n", "ONBUILD"},
		{"an unknown instruction", "FROM cbe/cpp20\nRUNN true\n", "not a Containerfile instruction"},
		{"two digests", "FROM cbe/cpp20@" + digest + " AS a\nFROM cbe/cpp20@sha256:" + strings.Repeat("1", 64) + "\n", "different digests"},
		{"too many words", "FROM cbe/cpp20 as a b\n", "must be written"},
	}
	for _, c := range cases {
		got := Check(c.text, "cbe/cpp20")
		joined := strings.Join(got.Problems, "\n")
		switch {
		case c.problem == "" && len(got.Problems) > 0:
			t.Errorf("%s: refused: %s", c.name, joined)
		case c.problem != "" && !strings.Contains(joined, c.problem):
			t.Errorf("%s: problems %q do not say %q", c.name, got.Problems, c.problem)
		}
	}
	if got := Check("FROM cbe/cpp20@"+digest+"\n", "cbe/cpp20"); got.Digest != digest || got.Stages != 1 {
		t.Errorf("digest and stages: %+v", got)
	}
	if got := Check("FROM cbe/cpp20\n", "cbe/cpp20"); got.Digest != "" {
		t.Errorf("a digest that is not written: %+v", got)
	}
	// Each problem says its line.
	if got := Check("FROM cbe/cpp20\n\nVOLUME /x\n", "cbe/cpp20"); len(got.Problems) != 1 || !strings.HasPrefix(got.Problems[0], "line 3: ") {
		t.Errorf("the line of a problem: %q", got.Problems)
	}
}

func TestWarnings(t *testing.T) {
	cases := []struct {
		name, run string
		want      string // a text a warning must contain ("" for no warning)
	}{
		{"apt without versions", "apt-get update && apt-get install -y --no-install-recommends libfoo-dev make", "libfoo-dev, make"},
		{"apt with versions", "apt-get install -y libfoo-dev=1.2-3 make=4.3-4", ""},
		{"apt with one version missing", "apt-get install -y libfoo-dev=1.2-3 make", "installs make "},
		{"apt with an option that takes a value", "apt-get -o Acquire::Retries=3 install -y make=4.3", ""},
		{"apt from a dated snapshot", "apt-get install -y make", ""},
		{"apt install of a variable", "apt-get install -y $PACKAGES", ""},
		{"apk", "apk add --no-cache git", "installs git "},
		{"apk pinned", "apk add --no-cache git=2.45.2-r0", ""},
		{"pip", "pip install numpy==1.26.4 scipy", "installs scipy "},
		{"pip3 through python -m", "python3 -m pip install requests", "installs requests "},
		{"pip with a requirements file", "pip install -r requirements.txt", ""},
		{"pip with a path", "pip install ./lib", ""},
		{"npm", "npm install -g typescript", "installs typescript "},
		{"npm pinned and scoped", "npm install -g typescript@5.4.5 @types/node@20.1.0", ""},
		{"npm scoped without a version", "npm install @types/node", "installs @types/node "},
		{"npm of the project", "npm install", ""},
		{"gem", "gem install rake", "installs rake "},
		{"gem with a version", "gem install rake -v 13.0.6", ""},
		{"a command that is not an installer", "echo apt-get install x", ""},
		{"installs in a chain", "make clean && pip install a==1 b", "installs b "},
	}
	for _, c := range cases {
		text := "FROM cbe/cpp20\nRUN " + c.run + "\n"
		if c.name == "apt from a dated snapshot" {
			text = "FROM cbe/cpp20\nRUN echo 'URIs: https://snapshot.ubuntu.com/ubuntu/20261105T000000Z' > /etc/apt/sources.list.d/s.sources\nRUN " + c.run + "\n"
		}
		got := Check(text, "cbe/cpp20")
		if len(got.Problems) > 0 {
			t.Errorf("%s: refused: %v", c.name, got.Problems)
		}
		joined := strings.Join(got.Warnings, "\n")
		switch {
		case c.want == "" && len(got.Warnings) > 0:
			t.Errorf("%s: warned: %s", c.name, joined)
		case c.want != "" && !strings.Contains(joined, c.want):
			t.Errorf("%s: warnings %q do not say %q", c.name, got.Warnings, c.want)
		}
	}
	// A heredoc is read as shell too, and ADD of a URL is said.
	got := Check("FROM cbe/cpp20\nRUN <<EOF\napt-get install -y make\nEOF\nADD https://example.org/x.tar.gz /opt/\nADD --checksum=sha256:"+strings.Repeat("a", 64)+" https://example.org/y.tar.gz /opt/\n", "cbe/cpp20")
	joined := strings.Join(got.Warnings, "\n")
	if !strings.Contains(joined, "installs make") || !strings.Contains(joined, "x.tar.gz") || strings.Contains(joined, "y.tar.gz") || len(got.Warnings) != 2 {
		t.Errorf("here-document and ADD warnings: %q", got.Warnings)
	}
}

func TestPin(t *testing.T) {
	ref := "localhost/cbe/cpp20@" + digest
	text := "# one\nFROM cbe/cpp20 AS build\nRUN make\nFROM cbe/cpp20@sha256:" + strings.Repeat("a", 64) + " \\\n  AS final\nCOPY --from=build /a /b\nFROM build AS third\nUSER root\n"
	got, err := Pin(text, "cbe/cpp20", ref)
	if err != nil {
		t.Fatal(err)
	}
	want := "# one\nFROM " + ref + " AS build\nRUN make\nFROM " + ref + " AS final\n\nCOPY --from=build /a /b\nFROM build AS third\nUSER root\n"
	if got != want {
		t.Errorf("pinned:\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "\n") != strings.Count(text, "\n") {
		t.Error("the line numbers moved")
	}
	// What was pinned is a file the checker takes as the build will see it: the course image by its full reference is not a course image
	// name, which is why the runner checks before it pins and never after.
	if _, err := Pin("\x00", "cbe/cpp20", ref); err == nil {
		t.Error("a file that does not parse was pinned")
	}
}
