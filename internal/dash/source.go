package dash

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The dashboard's own documentation is the specification here: "all public,
// static, non-final fields of the class will be automatically added as
// configuration variables", under @Config's value or the class's simple name,
// and classes marked @Disabled are ignored.
//
// Read from source rather than from the APK. What is wanted is what the file
// says now, which is the thing a tuned value has to be written back into.

var (
	configRe   = regexp.MustCompile(`@Config\s*(?:\(\s*"([^"]*)"\s*\))?`)
	disabledRe = regexp.MustCompile(`@Disabled\b`)
	classRe    = regexp.MustCompile(`\b(?:class|interface|enum)\s+(\w+)`)

	// public static, with an initialiser, capturing the declarator list rather
	// than one name: `double hP = 1.2, hI = 0` declares two fields and reading
	// only the first loses the rest. A field with no initialiser holds a type
	// default the source does not state, so there is nothing to compare.
	fieldRe = regexp.MustCompile(`(?m)^[ \t]*public\s+static\s+(?:final\s+)?[\w.$<>\[\], ?]+?\s+([A-Za-z_$][\w$]*\s*=[^;]*);`)

	finalRe = regexp.MustCompile(`\bfinal\b`)
)

// Field is one tunable as the source declares it.
type Field struct {
	// Section is what the dashboard files it under.
	Section string
	// Name is the field name.
	Name string
	// Value is the initialiser, normalised for comparison.
	Value string
	// Computed marks an initialiser that is an expression rather than a value,
	// which cannot be compared against what the robot evaluated it to.
	Computed bool
	// File is where it is declared.
	File string
	// Line is the 1-indexed line of the declaration.
	Line int
}

// Key is how this field is addressed in the dashboard.
func (f Field) Key() string { return f.Section + "." + f.Name }

// Source is every tunable a project declares.
type Source map[string]Field

// FromProject reads every @Config class under root.
func FromProject(root string) Source {
	out := Source{}

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case "build", ".git", ".gradle", ".idea":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".java") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		for _, field := range FromFile(path, string(content)) {
			out[field.Key()] = field
		}
		return nil
	})

	return out
}

// FromFile reads the tunables one java file declares.
func FromFile(path, content string) []Field {
	head := configRe.FindStringSubmatchIndex(content)
	if head == nil || disabledRe.MatchString(content[:head[0]]) {
		return nil
	}

	section := ""
	if head[2] >= 0 {
		section = content[head[2]:head[3]]
	}

	rest := content[head[1]:]

	if section == "" {
		name := classRe.FindStringSubmatch(rest)
		if name == nil {
			return nil
		}
		section = name[1]
	}

	// Structure is read off a copy with comments and literals blanked, so a
	// brace inside either cannot move the depth. Offsets still line up, so the
	// values themselves come out of the original.
	masked := mask(rest)

	body := strings.Index(masked, "{")
	if body < 0 {
		return nil
	}

	var out []Field
	for _, at := range fieldRe.FindAllStringSubmatchIndex(masked, -1) {
		if at[0] < body {
			continue
		}
		// Only the annotated class's own fields. Anything deeper belongs to a
		// nested class, which the dashboard does not read through this one.
		if depth(masked[body:at[0]]) != 1 {
			continue
		}
		if finalRe.MatchString(masked[at[0]:at[2]]) {
			continue
		}

		line := 1 + strings.Count(content[:head[1]]+rest[:at[0]], "\n")

		for _, part := range declarators(masked[at[2]:at[3]]) {
			name, value, found := strings.Cut(rest[at[2]+part[0]:at[2]+part[1]], "=")
			if !found {
				continue
			}

			name = strings.TrimSpace(name)
			if !isIdentifier(name) {
				continue
			}

			out = append(out, Field{
				Section:  section,
				Name:     name,
				Value:    Normalise(strings.TrimSpace(value)),
				Computed: !isLiteral(strings.TrimSpace(value)),
				File:     path,
				Line:     line,
			})
		}
	}

	return out
}

// declarators splits one declaration into its parts, as offsets into it.
//
// Commas inside a call, a generic or an array initialiser separate arguments
// rather than fields, so only the ones at the top level count.
func declarators(list string) [][2]int {
	var out [][2]int

	level, start := 0, 0
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '(', '[', '{', '<':
			level++
		case ')', ']', '}', '>':
			level--
		case ',':
			if level == 0 {
				out = append(out, [2]int{start, i})
				start = i + 1
			}
		}
	}

	return append(out, [2]int{start, len(list)})
}

// isLiteral reports whether an initialiser states its value outright.
//
// `Math.toRadians(3)` does not. The robot reports what it evaluated to, the
// source says how it was worked out, and comparing those as text would report a
// change on every run. A field like that is left out of the comparison rather
// than reported wrongly.
func isLiteral(value string) bool {
	switch value {
	case "true", "false", "":
		return value != ""
	}

	if strings.HasPrefix(value, `"`) {
		return strings.HasSuffix(value, `"`) && len(value) >= 2
	}

	if looksNumeric(value) {
		return true
	}

	// An enum constant, qualified or not.
	for _, part := range strings.Split(value, ".") {
		if !isIdentifier(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

// depth counts how deep in braces the end of a run of code is.
func depth(code string) int {
	level := 0
	for _, r := range code {
		switch r {
		case '{':
			level++
		case '}':
			level--
		}
	}
	return level
}

// Normalise renders a java initialiser the way the dashboard reports the same
// value, so the two can be compared as text.
func Normalise(value string) string {
	value = strings.TrimSpace(value)

	switch value {
	case "true", "false":
		return value
	}

	if strings.HasPrefix(value, `"`) {
		return value
	}

	if looksNumeric(value) {
		return Number(value)
	}

	// An enum reads as its constant name on the wire, however the source
	// qualified it.
	if i := strings.LastIndex(value, "."); i >= 0 && isIdentifier(value[i+1:]) {
		return value[i+1:]
	}

	return value
}

func looksNumeric(value string) bool {
	if value == "" {
		return false
	}

	body := strings.TrimPrefix(strings.TrimPrefix(value, "-"), "+")
	if body == "" {
		return false
	}

	for _, r := range body {
		if (r >= '0' && r <= '9') || r == '.' || r == '_' ||
			strings.ContainsRune("eExXaAbBcCdDfFlL+-", r) {
			continue
		}
		return false
	}

	return body[0] >= '0' && body[0] <= '9' || body[0] == '.'
}

func isIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for i, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '$':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// mask blanks out comments and literals, keeping every other byte where it was.
func mask(code string) string {
	out := []byte(code)

	const (
		plain = iota
		lineComment
		blockComment
		str
		char
	)

	state := plain
	for i := 0; i < len(code); i++ {
		c := code[i]

		switch state {
		case plain:
			switch {
			case c == '/' && i+1 < len(code) && code[i+1] == '/':
				state = lineComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '/' && i+1 < len(code) && code[i+1] == '*':
				state = blockComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '"':
				state = str
			case c == '\'':
				state = char
			}

		case lineComment:
			if c == '\n' {
				state = plain
			} else {
				out[i] = ' '
			}

		case blockComment:
			if c == '*' && i+1 < len(code) && code[i+1] == '/' {
				state = plain
				out[i], out[i+1] = ' ', ' '
				i++
			} else if c != '\n' {
				out[i] = ' '
			}

		case str, char:
			quote := byte('"')
			if state == char {
				quote = '\''
			}
			if c == '\\' && i+1 < len(code) {
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			if c == quote {
				state = plain
				continue
			}
			out[i] = ' '
		}
	}

	return string(out)
}
