// Command mdlinkcheck validates repository-local links in Markdown files.
// It never performs network requests; absolute URLs and non-file schemes are
// deliberately outside its scope.
package main

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	referenceDefinitionPattern = regexp.MustCompile(`^[ \t]{0,3}\[([^\]]+)\]:[ \t]*(.*)$`)
	atxHeadingPattern          = regexp.MustCompile(`^[ \t]{0,3}#{1,6}[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	setextHeadingPattern       = regexp.MustCompile(`^[ \t]{0,3}(?:=+|-+)[ \t]*$`)
	htmlAnchorPattern          = regexp.MustCompile(`(?i)<[a-z][^>]*\s(?:id|name)\s*=\s*["']([^"']+)["'][^>]*>`)
	htmlTagPattern             = regexp.MustCompile(`<[^>]+>`)
	inlineLinkTextPattern      = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	referenceLinkTextPattern   = regexp.MustCompile(`!?\[([^\]]*)\]\[[^\]]*\]`)
)

type problem struct {
	file    string
	line    int
	message string
}

type checkStats struct {
	files      int
	localLinks int
}

type linkTarget struct {
	raw  string
	line int
}

type referenceDefinition struct {
	target linkTarget
	label  string
}

type referenceUse struct {
	label string
	line  int
}

type checker struct {
	root        string
	anchorCache map[string]map[string]struct{}
	stats       checkStats
	problems    []problem
}

func main() {
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: mdlinkcheck [repository-root]")
		os.Exit(2)
	}
	root := "."
	if len(os.Args) == 2 {
		root = os.Args[1]
	}

	stats, problems, err := checkRepository(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdlinkcheck: %v\n", err)
		os.Exit(2)
	}
	for _, item := range problems {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", item.file, item.line, item.message)
	}
	if len(problems) != 0 {
		fmt.Fprintf(os.Stderr, "Markdown links: %d problem(s) in %d file(s)\n", len(problems), stats.files)
		os.Exit(1)
	}
	fmt.Printf("Markdown links: checked %d local link(s) in %d file(s)\n", stats.localLinks, stats.files)
}

func checkRepository(root string) (checkStats, []problem, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return checkStats{}, nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return checkStats{}, nil, err
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "artifacts", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !isMarkdownPath(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return checkStats{}, nil, err
	}
	slices.Sort(files)

	c := &checker{root: root, anchorCache: make(map[string]map[string]struct{})}
	for _, path := range files {
		if err := c.checkDocument(path); err != nil {
			return checkStats{}, nil, err
		}
	}
	slices.SortFunc(c.problems, func(a, b problem) int {
		if result := strings.Compare(a.file, b.file); result != 0 {
			return result
		}
		if a.line < b.line {
			return -1
		}
		if a.line > b.line {
			return 1
		}
		return strings.Compare(a.message, b.message)
	})
	return c.stats, c.problems, nil
}

func (c *checker) checkDocument(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c.stats.files++
	text := maskCode(string(data))
	definitions, definitionProblems := parseReferenceDefinitions(text)
	for _, item := range definitionProblems {
		c.addProblem(path, item.line, item.message)
	}
	links, uses := parseLinks(text, definitions)
	for _, link := range links {
		c.checkTarget(path, link)
	}
	for _, definition := range definitions {
		c.checkTarget(path, definition.target)
	}
	for _, use := range uses {
		if _, ok := definitions[normalizeReferenceLabel(use.label)]; !ok {
			c.addProblem(path, use.line, fmt.Sprintf("reference link [%s] has no definition", use.label))
		}
	}
	return nil
}

func (c *checker) checkTarget(sourcePath string, link linkTarget) {
	raw := strings.TrimSpace(link.raw)
	if raw == "" {
		return
	}
	parsed, err := url.Parse(html.UnescapeString(raw))
	if err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("invalid link target %q: %v", raw, err))
		return
	}
	if parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(raw, "//") || strings.HasPrefix(parsed.Path, "/") {
		return
	}

	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("invalid URL encoding in %q: %v", raw, err))
		return
	}
	decodedPath = unescapeMarkdown(decodedPath)
	fragment := parsed.Fragment
	if decodedPath == "" && fragment == "" {
		return
	}

	targetPath := sourcePath
	if decodedPath != "" {
		targetPath = filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(decodedPath)))
		rel, relErr := filepath.Rel(c.root, targetPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q escapes the repository", raw))
			return
		}
	}

	if err := requireExactPath(c.root, targetPath); err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q: %v", raw, err))
		return
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q: %v", raw, friendlyPathError(err)))
		return
	}
	if info.IsDir() {
		// A directory is itself a valid GitHub link. A fragment, however,
		// addresses the directory's rendered README and therefore requires it.
		if fragment == "" {
			if err := requireResolvedInside(c.root, targetPath); err != nil {
				c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q: %v", raw, err))
				return
			}
			c.stats.localLinks++
			return
		}
		targetPath = filepath.Join(targetPath, "README.md")
		if err := requireExactPath(c.root, targetPath); err != nil {
			c.addProblem(sourcePath, link.line, fmt.Sprintf("directory link %q: %v", raw, err))
			return
		}
		info, err = os.Stat(targetPath)
		if err != nil {
			c.addProblem(sourcePath, link.line, fmt.Sprintf("directory link %q has no README.md", raw))
			return
		}
	}
	if !info.Mode().IsRegular() {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q does not target a regular file", raw))
		return
	}

	if err := requireResolvedInside(c.root, targetPath); err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q: %v", raw, err))
		return
	}

	c.stats.localLinks++
	if fragment == "" || !isMarkdownPath(targetPath) {
		return
	}
	anchors, err := c.anchors(targetPath)
	if err != nil {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q: %v", raw, err))
		return
	}
	if _, ok := anchors[fragment]; !ok {
		c.addProblem(sourcePath, link.line, fmt.Sprintf("local link %q has no matching Markdown anchor %q", raw, fragment))
	}
}

func (c *checker) anchors(path string) (map[string]struct{}, error) {
	if cached, ok := c.anchorCache[path]; ok {
		return cached, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Heading text inside inline code contributes to GitHub-style anchors. Only
	// fenced code blocks must be hidden while discovering headings.
	anchors := markdownAnchors(maskMarkdownCode(string(data), false))
	c.anchorCache[path] = anchors
	return anchors, nil
}

func (c *checker) addProblem(path string, line int, message string) {
	rel, err := filepath.Rel(c.root, path)
	if err != nil {
		rel = path
	}
	c.problems = append(c.problems, problem{file: filepath.ToSlash(rel), line: line, message: message})
}

type parsedProblem struct {
	line    int
	message string
}

func parseReferenceDefinitions(text string) (map[string]referenceDefinition, []parsedProblem) {
	definitions := make(map[string]referenceDefinition)
	var problems []parsedProblem
	for lineIndex, line := range strings.Split(text, "\n") {
		matches := referenceDefinitionPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		label := normalizeReferenceLabel(matches[1])
		destination, ok := firstDestination(matches[2])
		if !ok {
			problems = append(problems, parsedProblem{line: lineIndex + 1, message: fmt.Sprintf("reference definition [%s] has no destination", matches[1])})
			continue
		}
		if _, exists := definitions[label]; exists {
			problems = append(problems, parsedProblem{line: lineIndex + 1, message: fmt.Sprintf("duplicate reference definition [%s]", matches[1])})
			continue
		}
		definitions[label] = referenceDefinition{
			label:  matches[1],
			target: linkTarget{raw: destination, line: lineIndex + 1},
		}
	}
	return definitions, problems
}

func parseLinks(text string, definitions map[string]referenceDefinition) ([]linkTarget, []referenceUse) {
	var links []linkTarget
	var uses []referenceUse
	for index := 0; index < len(text); {
		open := index
		if text[index] == '!' && index+1 < len(text) && text[index+1] == '[' {
			open++
		} else if text[index] != '[' {
			index++
			continue
		}
		if escaped(text, open) {
			index = open + 1
			continue
		}
		closeBracket := findClosing(text, open, '[', ']')
		if closeBracket < 0 {
			index = open + 1
			continue
		}
		labelText := text[open+1 : closeBracket]
		next := closeBracket + 1
		line := lineAt(text, open)
		switch {
		case next < len(text) && text[next] == '(':
			closeParen := findClosing(text, next, '(', ')')
			if closeParen < 0 {
				index = next + 1
				continue
			}
			if destination, ok := firstDestination(text[next+1 : closeParen]); ok {
				links = append(links, linkTarget{raw: destination, line: line})
			}
			index = closeParen + 1
		case next < len(text) && text[next] == '[':
			closeReference := findClosing(text, next, '[', ']')
			if closeReference < 0 {
				index = next + 1
				continue
			}
			referenceLabel := text[next+1 : closeReference]
			if referenceLabel == "" {
				referenceLabel = labelText
			}
			uses = append(uses, referenceUse{label: referenceLabel, line: line})
			index = closeReference + 1
		default:
			// Shortcut references cannot be distinguished from ordinary brackets
			// unless a matching definition exists, so only recognize defined ones.
			if _, ok := definitions[normalizeReferenceLabel(labelText)]; ok {
				uses = append(uses, referenceUse{label: labelText, line: line})
			}
			index = closeBracket + 1
		}
	}
	return links, uses
}

func firstDestination(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if value[0] == '<' {
		for index := 1; index < len(value); index++ {
			if value[index] == '>' && !escaped(value, index) {
				return value[1:index], true
			}
		}
		return "", false
	}
	for index := 0; index < len(value); index++ {
		if isASCIIWhitespace(value[index]) && !escaped(value, index) {
			return value[:index], true
		}
	}
	return value, true
}

func findClosing(text string, open int, opening, closing byte) int {
	depth := 0
	for index := open; index < len(text); index++ {
		if escaped(text, index) {
			continue
		}
		switch text[index] {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func escaped(text string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && text[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func lineAt(text string, offset int) int { return strings.Count(text[:offset], "\n") + 1 }

func normalizeReferenceLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

func maskCode(text string) string {
	return maskMarkdownCode(text, true)
}

func maskMarkdownCode(text string, maskInline bool) string {
	lines := strings.SplitAfter(text, "\n")
	inFence := false
	var fence byte
	var fenceLength int
	for lineIndex, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		marker, count := fenceMarker(trimmed)
		isFence := indent <= 3 && count >= 3
		isClosingFence := isFence && inFence && marker == fence && count >= fenceLength &&
			strings.TrimSpace(trimmed[count:]) == ""
		if isFence && (!inFence || isClosingFence) {
			if isClosingFence {
				inFence = false
			} else {
				inFence = true
				fence = marker
				fenceLength = count
			}
			lines[lineIndex] = blankPreservingNewline(line)
			continue
		}
		if inFence {
			lines[lineIndex] = blankPreservingNewline(line)
			continue
		}
		if maskInline {
			lines[lineIndex] = maskInlineCode(line)
		}
	}
	return strings.Join(lines, "")
}

func fenceMarker(line string) (byte, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}
	marker := line[0]
	count := 0
	for count < len(line) && line[count] == marker {
		count++
	}
	return marker, count
}

func blankPreservingNewline(line string) string {
	if strings.HasSuffix(line, "\n") {
		return "\n"
	}
	return ""
}

func maskInlineCode(line string) string {
	masked := []byte(line)
	for start := 0; start < len(line); {
		if line[start] != '`' || escaped(line, start) {
			start++
			continue
		}
		run := 1
		for start+run < len(line) && line[start+run] == '`' {
			run++
		}
		end := strings.Index(line[start+run:], strings.Repeat("`", run))
		if end < 0 {
			start += run
			continue
		}
		end += start + run
		for index := start; index < end+run; index++ {
			if masked[index] != '\n' {
				masked[index] = ' '
			}
		}
		start = end + run
	}
	return string(masked)
}

func markdownAnchors(text string) map[string]struct{} {
	anchors := make(map[string]struct{})
	counts := make(map[string]int)
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		for _, match := range htmlAnchorPattern.FindAllStringSubmatch(line, -1) {
			anchors[html.UnescapeString(match[1])] = struct{}{}
		}

		var heading string
		if matches := atxHeadingPattern.FindStringSubmatch(line); matches != nil {
			heading = matches[1]
		} else if index > 0 && setextHeadingPattern.MatchString(line) {
			heading = strings.TrimSpace(lines[index-1])
		}
		if heading == "" {
			continue
		}
		base := githubSlug(heading)
		if base == "" {
			continue
		}
		slug := base
		if duplicate := counts[base]; duplicate != 0 {
			slug = fmt.Sprintf("%s-%d", base, duplicate)
		}
		counts[base]++
		anchors[slug] = struct{}{}
	}
	return anchors
}

func githubSlug(heading string) string {
	heading = html.UnescapeString(heading)
	heading = inlineLinkTextPattern.ReplaceAllString(heading, "$1")
	heading = referenceLinkTextPattern.ReplaceAllString(heading, "$1")
	heading = htmlTagPattern.ReplaceAllString(heading, "")
	heading = strings.ToLower(unescapeMarkdown(heading))

	var builder strings.Builder
	previousSpace := false
	for len(heading) != 0 {
		r, size := utf8.DecodeRuneInString(heading)
		heading = heading[size:]
		if unicode.IsSpace(r) {
			previousSpace = true
			continue
		}
		if shouldDropFromSlug(r) {
			continue
		}
		if previousSpace {
			builder.WriteByte('-')
			previousSpace = false
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func shouldDropFromSlug(r rune) bool {
	if r == '-' || r == '_' {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func unescapeMarkdown(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+1 < len(value) {
			next := value[index+1]
			if strings.ContainsRune(`!"#$%&'()*+,-./:;<=>?@[\]^_`+"`"+`{|}~`, rune(next)) {
				builder.WriteByte(next)
				index++
				continue
			}
		}
		builder.WriteByte(value[index])
	}
	return builder.String()
}

func requireExactPath(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	current := root
	for component := range strings.SplitSeq(rel, string(filepath.Separator)) {
		entries, readErr := os.ReadDir(current)
		if readErr != nil {
			return friendlyPathError(readErr)
		}
		found := false
		caseInsensitiveMatch := ""
		for _, entry := range entries {
			if entry.Name() == component {
				found = true
				break
			}
			if strings.EqualFold(entry.Name(), component) {
				caseInsensitiveMatch = entry.Name()
			}
		}
		if !found {
			if caseInsensitiveMatch != "" {
				return fmt.Errorf("path component %q has wrong case; want %q", component, caseInsensitiveMatch)
			}
			return fmt.Errorf("target does not exist (missing path component %q)", component)
		}
		current = filepath.Join(current, component)
	}
	return nil
}

func isASCIIWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func friendlyPathError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("target does not exist")
	}
	return err
}

func requireResolvedInside(root, target string) error {
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return friendlyPathError(err)
	}
	rel, err := filepath.Rel(root, realTarget)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("resolves outside the repository")
	}
	return nil
}

func isMarkdownPath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".md" || extension == ".markdown"
}
