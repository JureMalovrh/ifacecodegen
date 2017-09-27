package ifacecodegen

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	goimports "golang.org/x/tools/imports"
)

// GenerateOptions are options used to generate code
type GenerateOptions struct {
	Source          string
	Package         *Package
	Interfaces      []string
	Template        io.Reader
	OverridePackage string
	Imports         []*Import
	Meta            map[string]string
	Functions       template.FuncMap
}

// Generate generates code
func Generate(opts GenerateOptions) ([]byte, error) {

	if opts.Package == nil {
		return nil, fmt.Errorf("Package is nil")
	}

	if opts.Template == nil {
		return nil, fmt.Errorf("Template is nil")
	}

	if opts.Functions == nil {
		opts.Functions = template.FuncMap{}
	}

	g := &generator{
		opts: opts,
	}

	err := g.Generate()
	if err != nil {
		return nil, err
	}

	return g.Output()
}

type generator struct {
	buf bytes.Buffer

	opts GenerateOptions
}

func (g *generator) Generate() error {
	g.p("// Code generated by ifacecodegen tool. DO NOT EDIT.")
	g.p("// Source: %v", g.opts.Source)
	g.p("")

	pkgName := sanitize(g.opts.Package.Name)
	if g.opts.OverridePackage != "" {
		pkgName = g.opts.OverridePackage
	}
	g.p("package %v", pkgName)
	g.p("")
	g.p("import (")
	for _, impPkg := range g.opts.Imports {
		if pkgName == impPkg.Path {
			continue
		}
		g.p("%v %q", impPkg.Path, impPkg.Package)
	}
	g.p(")")

	trimPackageNameRegExp := regexp.MustCompile(fmt.Sprintf(`([\]\*\. ]|^)%s\.`, pkgName))
	parameterName := func(p *Parameter) string {
		return trimPackageNameRegExp.ReplaceAllString(p.Type.String(), "${1}")
	}

	outputVarType := func(m Method, typeName string) string {
		for _, out := range m.Out {
			if parameterName(out) == typeName {
				return out.Name
			}
		}

		return ""
	}

	funcMap := g.opts.Functions

	funcMap["meta"] = func(key string, defaultValue ...string) string {
		meta, ok := g.opts.Meta[key]
		if ok {
			return meta
		}

		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return ""
	}

	funcMap["parameter"] = parameterName

	funcMap["input_var_type"] = func(m Method, typ string) string {
		for _, in := range m.In {
			if parameterName(in) == typ {
				return in.Name
			}
		}

		return ""
	}
	funcMap["input_parameters"] = func(m Method) string {
		ret := make([]string, 0, len(m.In))

		for _, in := range m.In {
			ret = append(ret, in.Name+" "+parameterName(in))
		}

		return strings.Join(ret, ", ")
	}
	funcMap["input_calls"] = func(m Method) string {
		ret := make([]string, 0, len(m.In))

		for _, in := range m.In {
			name := in.Name
			if strings.HasPrefix(parameterName(in), "...") {
				name += "..."
			}
			ret = append(ret, name)
		}

		return strings.Join(ret, ", ")
	}
	funcMap["output_vars"] = func(m Method) string {
		ret := make([]string, 0, len(m.Out))

		for _, out := range m.Out {
			ret = append(ret, out.Name)
		}

		retString := strings.Join(ret, ", ")

		return retString
	}
	funcMap["output_parameters"] = func(m Method) string {
		ret := make([]string, 0, len(m.Out))

		for _, out := range m.Out {
			ret = append(ret, out.Name+" "+parameterName(out))
		}

		return fmt.Sprintf("(%s)", strings.Join(ret, ", "))
	}

	funcMap["output_var_error"] = func(m Method) string {
		return outputVarType(m, "error")
	}

	funcMap["output_var_type"] = outputVarType

	funcMap["return"] = func(m Method) string {
		if len(m.Out) > 0 {
			return "return"
		}

		return ""
	}

	templateContent, err := ioutil.ReadAll(g.opts.Template)
	if err != nil {
		return err
	}

	t := template.Must(template.New("template").Funcs(funcMap).Parse(string(templateContent)))

	shouldGenerateInterface := func(_ string) bool {
		return true
	}
	if len(g.opts.Interfaces) > 0 {
		shouldGenerateInterface = func(name string) bool {
			for _, a := range g.opts.Interfaces {
				if a == name {
					return true
				}
			}
			return false
		}
	}

	for _, intf := range g.opts.Package.Interfaces {
		if !shouldGenerateInterface(intf.Name) {
			continue
		}

		err := t.ExecuteTemplate(&g.buf, "template", intf)
		if err != nil {
			return err
		}
		g.buf.WriteString("\n")
	}

	return nil
}

func (g *generator) p(format string, args ...interface{}) {
	fmt.Fprintf(&g.buf, format+"\n", args...)
}

// Output returns the generator's output, formatted in the standard Go style.
func (g *generator) Output() ([]byte, error) {

	data := g.buf.Bytes()
	options := &goimports.Options{
		TabWidth:  8,
		TabIndent: true,
		Comments:  true,
		Fragment:  true,
	}
	src, err := goimports.Process("", data, options)
	if err != nil {
		return nil, fmt.Errorf("Failed to run imports on generated source code: %s\n%s", err, string(data))
	}
	data = src

	src, err = format.Source(data)
	if err != nil {
		return nil, fmt.Errorf("Failed to format generated source code: %s\n%s", err, string(data))
	}

	return src, nil
}

func sanitize(s string) string {
	t := ""
	for _, r := range s {
		if t == "" {
			if unicode.IsLetter(r) || r == '_' {
				t += string(r)
				continue
			}
		} else {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				t += string(r)
				continue
			}
		}
		t += "_"
	}
	if t == "_" {
		t = "x"
	}
	return t
}