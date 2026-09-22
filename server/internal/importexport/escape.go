package importexport

import "strings"

const escapedChars = `\|:`

func EscapeSubValue(s string) string {
	if !strings.ContainsAny(s, escapedChars) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '|' || c == ':' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func UnescapeSubValue(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		b.WriteByte(c)
	}
	if escaped {
		b.WriteByte('\\')
	}
	return b.String()
}

func SplitEscaped(s string, sep byte) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == sep:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

const headerEscapedChars = `\|:#`

func escapeColumnName(s string) string {
	if !strings.ContainsAny(s, headerEscapedChars) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(headerEscapedChars, c) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func joinEscaped(values []string, sep byte) string {
	if len(values) == 0 {
		return ""
	}
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = EscapeSubValue(v)
	}
	return strings.Join(escaped, string(sep))
}
