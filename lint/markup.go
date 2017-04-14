package lint

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/ValeLint/vale/core"
	"github.com/russross/blackfriday"
	"golang.org/x/net/html"
	"matloob.io/regexp"
)

// reStructuredText configuration.
//
// reCodeBlock is used to convert Sphinx-style code directives to the regular
// `::` for rst2html.
var reCodeBlock = regexp.MustCompile(`.. (?:raw|code(?:-block)?):: (\w+)`)
var rstArgs = []string{
	"--quiet",             // We don't want stdout being filled with messages.
	"--halt=5",            // We only want to fail when absolutely necessary.
	"--link-stylesheet",   // We don't need the stylesheet
	"--no-file-insertion", // We don't want extra content in the HTML.
	"--no-toc-backlinks",  // We don't want extra links or numbering.
	"--no-footnote-backlinks",
	"--no-section-numbering",
}

// AsciiDoc configuration.
var adocArgs = []string{
	"--no-header-footer", // We don't want extra content in the HTML.
	"--quiet",            // We don't want stdout being filled with messages.
	"--safe-mode",        // This disables `includes`, which we don't want
	"secure",
	"-", // Use stdin
}

// Blackfriday configuration.
var commonHTMLFlags = 0 | blackfriday.HTML_USE_XHTML
var commonExtensions = 0 |
	blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
	blackfriday.EXTENSION_TABLES |
	blackfriday.EXTENSION_FENCED_CODE
var renderer = blackfriday.HtmlRenderer(commonHTMLFlags, "", "")
var options = blackfriday.Options{Extensions: commonExtensions}

// HTML configuration.
var heading = regexp.MustCompile(`^h\d$`)
var skipTags = []string{"script", "style", "pre", "figure"}
var ignoreTags = []string{"code", "tt"}
var skipClasses = []string{}

func (l Linter) lintHTMLTokens(f *core.File, rawBytes []byte, fBytes []byte, offset int) {
	var txt, attr string
	var tokt html.TokenType
	var tok html.Token
	var inBlock, skip, ignore bool

	ctx := core.PrepText(string(rawBytes))
	lines := strings.Count(ctx, "\n") + 1 + offset
	// If your docs are `aimed` at helping people code, then
	// If your docs are ******* at helping people code, then
	buf := bytes.NewBufferString("")
	queue := []string{}

	tokens := html.NewTokenizer(bytes.NewReader(fBytes))
	for {
		tokt = tokens.Next()
		tok = tokens.Token()
		txt = core.PrepText(html.UnescapeString(strings.TrimSpace(tok.Data)))
		skip = core.StringInSlice(txt, skipTags) || core.StringInSlice(attr, skipClasses)
		if tokt == html.ErrorToken {
			break
		} else if tokt == html.StartTagToken && skip {
			inBlock = true
		} else if tokt == html.StartTagToken {
			ignore = core.StringInSlice(txt, ignoreTags)
		} else if skip && inBlock {
			inBlock = false
		} else if tokt == html.EndTagToken && !core.StringInSlice(txt, ignoreTags) {
			text := buf.String()
			if heading.MatchString(txt) {
				l.lintText(f, NewBlock(ctx, text, "heading"+f.RealExt), lines, 0)
			} else {
				l.lintProse(f, ctx, text, lines, 0)
			}
			for _, s := range queue {
				ctx = updateCtx(ctx, s, html.TextToken)
			}
			queue = []string{}
			buf.Reset()
		} else if tokt == html.TextToken && !inBlock {
			queue = append(queue, txt)
			if ignore {
				txt, _ = core.Substitute(txt, txt)
				txt = " " + txt + " "
				ignore = false
			}
			buf.WriteString(txt)
		} else if tokt == html.TextToken {
			ctx = updateCtx(ctx, txt, tokt)
		}
		attr = getAttribute(tok, "class")
		ctx = clearElements(ctx, tok)
	}
}

func getAttribute(tok html.Token, key string) string {
	for _, attr := range tok.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func clearElements(ctx string, tok html.Token) string {
	if tok.Data == "img" || tok.Data == "a" {
		for _, a := range tok.Attr {
			if a.Key == "alt" || a.Key == "href" {
				ctx = updateCtx(ctx, a.Val, html.TextToken)
			}
		}
	}
	return ctx
}

func updateCtx(ctx string, txt string, tokt html.TokenType) string {
	var found bool
	if (tokt == html.TextToken || tokt == html.CommentToken) && txt != "" {
		for _, s := range strings.Split(txt, "\n") {
			ctx, found = core.Substitute(ctx, s)
			if !found {
				for _, w := range strings.Fields(s) {
					ctx, _ = core.Substitute(ctx, w)
				}
			}
		}
	}
	return ctx
}

func (l Linter) lintHTML(f *core.File) {
	l.lintHTMLTokens(f, f.Content, f.Content, 0)
}

func (l Linter) lintMarkdown(f *core.File) {
	html := blackfriday.MarkdownOptions(f.Content, renderer, options)
	l.lintHTMLTokens(f, f.Content, html, 0)
}

func (l Linter) lintRST(f *core.File, python string, rst2html string) {
	var out bytes.Buffer
	cmd := exec.Command(python, append([]string{rst2html}, rstArgs...)...)
	cmd.Stdin = bytes.NewReader(reCodeBlock.ReplaceAll(f.Content, []byte("::")))
	cmd.Stdout = &out
	if core.CheckError(cmd.Run()) {
		l.lintHTMLTokens(f, f.Content, out.Bytes(), 0)
	}
}

func (l Linter) lintADoc(f *core.File, asciidoctor string) {
	var out bytes.Buffer
	cmd := exec.Command(asciidoctor, adocArgs...)
	cmd.Stdin = bytes.NewReader(f.Content)
	cmd.Stdout = &out
	if core.CheckError(cmd.Run()) {
		l.lintHTMLTokens(f, f.Content, out.Bytes(), 0)
	}
}
