package vhosts

import "strings"

func nginxLocation(indent, match string, directives ...string) string {
	step := "    "
	if strings.Contains(indent, "\t") {
		step = "\t"
	}
	var out strings.Builder
	out.WriteString(indent + "location " + match + " {\n")
	for _, directive := range directives {
		out.WriteString(indent + step + directive + "\n")
	}
	out.WriteString(indent + "}")
	return out.String()
}

func nginxServerIndent(content string, server AuthDirective) string {
	for _, child := range server.Children {
		if child.Name == "location" && strings.Contains(strings.Join(child.Args, " "), "acme-challenge") {
			continue
		}
		start := strings.LastIndexByte(content[:child.Start], '\n') + 1
		indent := content[start:child.Start]
		if strings.Trim(indent, " \t") == "" {
			return indent
		}
	}
	start := strings.LastIndexByte(content[:server.Start], '\n') + 1
	indent := content[start:server.Start]
	if strings.Trim(indent, " \t") != "" {
		indent = ""
	}
	return indent + "    "
}

// Adjust only the whitespace surrounding a generated block. Replacing a block
// must not accumulate blank lines or change unrelated directives and comments.
func nginxBlockEdit(content string, start, end int, indent, block string) configEdit {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		block = strings.ReplaceAll(block, "\n", newline)
	}
	if start == end {
		if offset := strings.IndexByte(content[start:], '\n'); offset >= 0 {
			rest := strings.TrimSpace(content[start : start+offset])
			if rest == "" || strings.HasPrefix(rest, "#") {
				// Keep comments on the opening brace's line in place.
				start += offset + 1
				return configEdit{start, start, block + newline}
			}
		}
	}
	line := strings.LastIndexByte(content[:start], '\n') + 1
	if start != end && strings.Trim(content[line:start], " \t") == "" {
		start = line
	} else {
		block = newline + block
	}
	next := end
	for next < len(content) && (content[next] == ' ' || content[next] == '\t') {
		next++
	}
	if next < len(content) && !strings.ContainsRune("\r\n#", rune(content[next])) {
		end = next
		block += newline + indent
	}
	return configEdit{start, end, block}
}
