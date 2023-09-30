package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/a-h/parse"
)

func Parse(fileName string) (TemplateFile, error) {
	fc, err := os.ReadFile(fileName)
	if err != nil {
		return TemplateFile{}, err
	}
	return ParseString(string(fc))
}

func getDefaultPackageName(fileName string) (pkg string) {
	parent := filepath.Base(filepath.Dir(fileName))
	if !isGoIdentifier(parent) {
		return "main"
	}
	return parent
}

func isGoIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, r := range s {
		if unicode.IsLetter(r) || r == '_' {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

func ParseString(template string) (TemplateFile, error) {
	tf, ok, err := NewTemplateFileParser("main").Parse(parse.NewInput(template))
	if err != nil {
		return tf, err
	}
	if !ok {
		err = ErrTemplateNotFound
	}
	return tf, err
}

// NewTemplateFileParser creates a new TemplateFileParser.
func NewTemplateFileParser(pkg string) TemplateFileParser {
	return TemplateFileParser{
		DefaultPackage: pkg,
	}
}

var ErrLegacyFileFormat = errors.New("Legacy file format - run templ migrate")
var ErrTemplateNotFound = errors.New("Template not found")

type TemplateFileParser struct {
	DefaultPackage string
}

func (p TemplateFileParser) Parse(pi *parse.Input) (tf TemplateFile, ok bool, err error) {
	// If we're parsing a legacy file, complain that migration needs to happen.
	_, ok, err = parse.String("{% package").Parse(pi)
	if err != nil {
		return
	}
	if ok {
		return tf, false, ErrLegacyFileFormat
	}

	// Optional whitespace and comments.
	for {
		var ws Whitespace
		ws.Value, ok, err = parse.Whitespace.Parse(pi)
		if err != nil {
			return tf, false, err
		}
		if ok {
			tf.Header = append(tf.Header, ws)
			continue
		}
		var c GoComment
		c, ok, err = goComment.Parse(pi)
		if err != nil {
			return tf, false, err
		}
		if ok {
			tf.Header = append(tf.Header, c)
			continue
		}
		break
	}

	// Comments.

	// Required package.
	// package name
	from := pi.Position()
	tf.Package, ok, err = pkg.Parse(pi)
	if err != nil {
		return
	}
	if !ok {
		tf.Package = Package{
			Expression: NewExpression("package "+p.DefaultPackage, from, pi.Position()),
		}
	}

	// Optional whitespace.
	_, _, _ = parse.OptionalWhitespace.Parse(pi)

outer:
	for {
		// Optional templates, CSS, and script templates.
		// templ Name(p Parameter)
		var tn HTMLTemplate
		tn, ok, err = template.Parse(pi)
		if err != nil {
			return tf, false, err
		}
		if ok {
			tf.Nodes = append(tf.Nodes, tn)
			_, _, _ = parse.OptionalWhitespace.Parse(pi)
			continue
		}

		// css Name()
		var cn CSSTemplate
		cn, ok, err = cssParser.Parse(pi)
		if err != nil {
			return tf, false, err
		}
		if ok {
			tf.Nodes = append(tf.Nodes, cn)
			_, _, _ = parse.OptionalWhitespace.Parse(pi)
			continue
		}

		// script Name()
		var sn ScriptTemplate
		sn, ok, err = scriptTemplateParser.Parse(pi)
		if err != nil {
			return tf, false, err
		}
		if ok {
			tf.Nodes = append(tf.Nodes, sn)
			_, _, _ = parse.OptionalWhitespace.Parse(pi)
			continue
		}

		// Anything that isn't template content is Go code.
		var code strings.Builder
		from := pi.Position()
	inner:
		for {
			// Check to see if this line isn't Go code.
			last := pi.Index()
			var l string
			if l, ok, err = parse.StringUntil(parse.Or(parse.NewLine, parse.EOF[string]())).Parse(pi); err != nil {
				return
			}
			hasTemplatePrefix := strings.HasPrefix(l, "templ ") || strings.HasPrefix(l, "css ") || strings.HasPrefix(l, "script ")
			if hasTemplatePrefix && strings.HasSuffix(l, "{") {
				// Unread the line.
				pi.Seek(last)
				// Take the code so far.
				if code.Len() > 0 {
					expr := NewExpression(strings.TrimSpace(code.String()), from, pi.Position())
					tf.Nodes = append(tf.Nodes, GoExpression{Expression: expr})
				}
				// Carry on parsing.
				break inner
			}
			code.WriteString(l)

			// Eat the newline or EOF that we read until.
			var newLine string
			if newLine, ok, err = parse.NewLine.Parse(pi); err != nil {
				return
			}
			code.WriteString(newLine)
			if _, isEOF, _ := parse.EOF[string]().Parse(pi); isEOF {
				if code.Len() > 0 {
					expr := NewExpression(strings.TrimSpace(code.String()), from, pi.Position())
					tf.Nodes = append(tf.Nodes, GoExpression{Expression: expr})
				}
				// Stop parsing.
				break outer
			}
		}
	}

	return tf, true, nil
}
