package workspace

import "testing"

func TestTheInstructionsBecomeMarkdown(t *testing.T) {
	for name, test := range map[string]struct{ html, want string }{
		"paragraph and emphasis": {`<p>Build with <code>make</code>. Run the <strong>sample</strong> tests with <em>care</em>.</p>`, "Build with `make`. Run the **sample** tests with *care*.\n"},
		"entities":               {`<p>a &lt; b &amp;&amp; c &gt; d</p>`, "a < b && c > d\n"},
		"two paragraphs":         {`<p>One.</p><p>Two.</p>`, "One.\n\nTwo.\n"},
		"a list":                 {`<ul><li>one</li><li>two <code>x</code></li></ul>`, "- one\n- two `x`\n"},
		"a numbered list":        {`<ol><li>first</li><li>second</li></ol>`, "1. first\n2. second\n"},
		"a nested list":          {`<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>`, "- a\n  - b\n- c\n"},
		"code block":             {"<pre><code class=\"language-cpp\">int x = 1;\nreturn x;\n</code></pre>", "```cpp\nint x = 1;\nreturn x;\n```\n"},
		"heading":                {`<h3>Part</h3><p>text</p>`, "### Part\n\ntext\n"},
		"quote":                  {`<blockquote><p>wise words</p></blockquote>`, "> wise words\n"},
		"a table":                {`<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>x|y</td></tr></tbody></table>`, "| A | B |\n| --- | --- |\n| 1 | x\\|y |\n"},
		"unknown tags lose tags": {`<p><span class="x">kept</span> <a href="#">text</a></p>`, "kept text\n"},
		"a picture":              {`<p><img src="a.png" alt="the list"></p>`, "[picture: the list]\n"},
		"nothing":                {``, "\n"},
	} {
		if got := markdownOf(test.html); got != test.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, test.want)
		}
	}
}
