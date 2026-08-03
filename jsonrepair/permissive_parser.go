package jsonrepair

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const maxPermissiveLookahead = 4096

type permissiveRole uint8

const (
	permissiveValue permissiveRole = iota
	permissiveKey
)

type permissiveParser struct {
	characters      []rune
	byteOffsets     []int
	nextSignificant []int
	position        int
	depth           int
	containers      []rune
	limits          Limits
}

type recoveredValue struct {
	value   any
	present bool
}

type recoveredString struct {
	value      string
	terminated bool
}

func newPermissiveParser(input []byte, limits Limits) *permissiveParser {
	characters := make([]rune, 0, utf8.RuneCount(input))
	byteOffsets := make([]int, 0, cap(characters)+1)
	remaining := input
	bytePosition := 0
	for len(remaining) > 0 {
		character, size := utf8.DecodeRune(remaining)
		characters = append(characters, normalizeJSONRune(character))
		byteOffsets = append(byteOffsets, bytePosition)
		remaining = remaining[size:]
		bytePosition += size
	}
	byteOffsets = append(byteOffsets, bytePosition)

	nextSignificant := make([]int, len(characters)+1)
	nextSignificant[len(characters)] = len(characters)
	for index := len(characters) - 1; index >= 0; index-- {
		if unicode.IsSpace(characters[index]) {
			nextSignificant[index] = nextSignificant[index+1]
		} else {
			nextSignificant[index] = index
		}
	}

	return &permissiveParser{
		characters:      characters,
		byteOffsets:     byteOffsets,
		nextSignificant: nextSignificant,
		limits:          limits,
	}
}

func normalizeJSONRune(character rune) rune {
	switch character {
	case '\u201c', '\u201d', '\u201e', '\uff02':
		return '"'
	case '\u2018', '\u2019':
		return '\''
	default:
		return character
	}
}

func canonicalStructuralRune(character rune) rune {
	switch character {
	case '\uff5b':
		return '{'
	case '\uff5d':
		return '}'
	case '\uff3b':
		return '['
	case '\uff3d':
		return ']'
	case '\uff1a':
		return ':'
	case '\uff0c':
		return ','
	default:
		return character
	}
}

func (parser *permissiveParser) parseDocument() (any, error) {
	start := parser.nextRoot(parser.position)
	if start < 0 {
		return "", nil
	}
	parser.position = start

	first, err := parser.parseValue()
	if err != nil {
		return nil, err
	}
	if !first.present {
		return "", nil
	}
	values := []any{first.value}

	for {
		next := parser.nextRoot(parser.position)
		if next < 0 {
			break
		}
		parser.position = next
		value, parseErr := parser.parseValue()
		if parseErr != nil {
			return nil, parseErr
		}
		if value.present {
			values = append(values, value.value)
		}
		if parser.position <= next {
			parser.position++
		}
	}

	if len(values) == 1 {
		return values[0], nil
	}
	return values, nil
}

func (parser *permissiveParser) nextRoot(from int) int {
	for index := from; index < len(parser.characters); index++ {
		character := canonicalStructuralRune(parser.characters[index])
		if character == '{' || character == '[' {
			return index
		}
	}
	return -1
}

func (parser *permissiveParser) parseValue() (recoveredValue, error) {
	parser.skipWhitespace()
	if parser.atEnd() {
		return recoveredValue{}, nil
	}

	switch parser.currentStructural() {
	case '{':
		value, err := parser.parseObject()
		return recoveredValue{value: value, present: err == nil}, err
	case '[':
		value, err := parser.parseArray()
		return recoveredValue{value: value, present: err == nil}, err
	case '"', '\'':
		value := parser.parseString(permissiveValue)
		if !value.terminated && value.value == "" {
			return recoveredValue{}, nil
		}
		return recoveredValue{value: value.value, present: true}, nil
	default:
		return parser.parseBareValue(), nil
	}
}

func (parser *permissiveParser) parseObject() (map[string]any, error) {
	if err := parser.enter(); err != nil {
		return nil, err
	}
	defer parser.leave()
	parser.containers = append(parser.containers, '{')
	defer func() { parser.containers = parser.containers[:len(parser.containers)-1] }()
	parser.position++

	object := make(map[string]any)
	for {
		parser.skipWhitespaceAndCommas()
		if parser.atEnd() {
			return object, nil
		}
		if parser.currentStructural() == '}' {
			parser.position++
			return object, nil
		}
		if parser.currentStructural() == ']' {
			return object, nil
		}

		if !parser.positionAtObjectKey() {
			parser.discardObjectJunk()
			continue
		}

		key, quoted := parser.parseObjectKey()
		parser.skipWhitespace()
		if parser.atEnd() {
			if key != "" {
				object[key] = ""
			}
			return object, nil
		}

		hasColon := parser.currentStructural() == ':'
		if hasColon {
			parser.position++
		} else if !quoted || !parser.canStartRecoveredValue(parser.current()) {
			parser.discardUntilValueOrBoundary()
			if !parser.atEnd() && parser.currentStructural() == ':' {
				parser.position++
				hasColon = true
			}
		}

		parser.skipWhitespace()
		if parser.atEnd() || parser.currentStructural() == ',' || parser.currentStructural() == '}' || parser.currentStructural() == ']' {
			if key != "" || hasColon {
				object[key] = ""
			}
			if !parser.atEnd() && parser.currentStructural() == ',' {
				parser.position++
				continue
			}
			if !parser.atEnd() && parser.currentStructural() == '}' {
				parser.position++
			}
			return object, nil
		}

		value, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		if value.present {
			object[key] = value.value
		} else {
			object[key] = ""
		}

		parser.skipWhitespace()
		if parser.atEnd() {
			return object, nil
		}
		switch parser.currentStructural() {
		case ',':
			parser.position++
		case '}':
			parser.position++
			return object, nil
		case ']':
			return object, nil
		default:
			// The next iteration either recognizes an omitted comma or skips
			// non-structural text before the next member.
		}
	}
}

func (parser *permissiveParser) parseArray() ([]any, error) {
	if err := parser.enter(); err != nil {
		return nil, err
	}
	defer parser.leave()
	parser.containers = append(parser.containers, '[')
	defer func() { parser.containers = parser.containers[:len(parser.containers)-1] }()
	parser.position++

	array := make([]any, 0)
	for {
		parser.skipWhitespaceAndCommas()
		if parser.atEnd() {
			return array, nil
		}
		if parser.currentStructural() == ']' {
			parser.position++
			return array, nil
		}
		if parser.currentStructural() == '}' {
			if parser.nextNonWhitespace(parser.position+1) < len(parser.characters) &&
				canonicalStructuralRune(parser.characters[parser.nextNonWhitespace(parser.position+1)]) == ']' {
				parser.position++
				continue
			}
			return array, nil
		}

		before := parser.position
		value, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		if value.present {
			array = append(array, value.value)
		}
		if parser.position <= before {
			parser.position++
		}

		parser.skipWhitespace()
		if parser.atEnd() {
			return array, nil
		}
		switch parser.currentStructural() {
		case ',':
			parser.position++
		case ']':
			parser.position++
			return array, nil
		case '}':
			if parser.nextNonWhitespace(parser.position+1) < len(parser.characters) &&
				canonicalStructuralRune(parser.characters[parser.nextNonWhitespace(parser.position+1)]) == ']' {
				parser.position++
			} else {
				return array, nil
			}
		default:
			// Continue to support a missing comma between recoverable values.
		}
	}
}

func (parser *permissiveParser) positionAtObjectKey() bool {
	if parser.atEnd() {
		return false
	}
	if parser.current() == '"' || parser.current() == '\'' {
		return true
	}

	index := parser.position
	for index < len(parser.characters) {
		character := canonicalStructuralRune(parser.characters[index])
		switch character {
		case ':':
			return index > parser.position
		case '"', '\'', ',', '}', ']':
			return false
		}
		if unicode.IsSpace(character) {
			lookahead := parser.nextNonWhitespace(index)
			return lookahead < len(parser.characters) && canonicalStructuralRune(parser.characters[lookahead]) == ':'
		}
		index++
	}
	return false
}

func (parser *permissiveParser) discardObjectJunk() {
	for !parser.atEnd() {
		switch parser.currentStructural() {
		case '"', '\'', '}', ']':
			return
		case ',':
			parser.position++
			return
		default:
			parser.position++
		}
	}
}

func (parser *permissiveParser) parseObjectKey() (string, bool) {
	if parser.current() == '"' || parser.current() == '\'' {
		return parser.parseString(permissiveKey).value, true
	}

	start := parser.position
	for !parser.atEnd() {
		character := parser.currentStructural()
		if character == ':' || character == ',' || character == '}' || character == ']' || unicode.IsSpace(character) {
			break
		}
		parser.position++
	}
	return cleanRecoveredText(parser.characters[start:parser.position]), false
}

func (parser *permissiveParser) discardUntilValueOrBoundary() {
	for !parser.atEnd() {
		character := parser.currentStructural()
		if character == ':' || character == ',' || character == '}' || character == ']' || parser.canStartRecoveredValue(character) {
			return
		}
		parser.position++
	}
}

func (parser *permissiveParser) parseString(role permissiveRole) recoveredString {
	opener := parser.current()
	parser.position++

	if !parser.atEnd() && parser.current() == opener {
		afterRun := parser.position
		for afterRun < len(parser.characters) && parser.characters[afterRun] == opener {
			afterRun++
		}
		afterRunNonWhitespace := parser.nextNonWhitespace(afterRun)
		separatedValue := afterRunNonWhitespace > afterRun && afterRunNonWhitespace < len(parser.characters) &&
			(parser.characters[afterRunNonWhitespace] == '"' || parser.characters[afterRunNonWhitespace] == '\'')
		if afterRun < len(parser.characters) && !separatedValue &&
			(afterRunNonWhitespace >= len(parser.characters) || !isStringBoundary(parser.characters[afterRunNonWhitespace], role)) {
			parser.position = afterRun
		}
	}

	var value strings.Builder
	for !parser.atEnd() {
		character := parser.current()
		if character == '\\' {
			parser.consumePermissiveEscape(&value, role)
			continue
		}
		if character == opener {
			runEnd := parser.position + 1
			for runEnd < len(parser.characters) && parser.characters[runEnd] == character {
				runEnd++
			}
			if runEnd-parser.position > 1 && parser.quoteRunCloses(runEnd, role) {
				parser.position = runEnd
				return recoveredString{value: value.String(), terminated: true}
			}
			if parser.quoteCloses(parser.position, role) {
				parser.position++
				return recoveredString{value: value.String(), terminated: true}
			}
			value.WriteRune(character)
			parser.position++
			continue
		}
		if character == '\n' || character == '\r' {
			if role != permissiveKey {
				value.WriteRune(character)
			}
			parser.position++
			continue
		}
		if character == utf8.RuneError {
			parser.position++
			continue
		}
		value.WriteRune(character)
		parser.position++
	}
	return recoveredString{value: value.String()}
}

func (parser *permissiveParser) quoteRunCloses(afterRun int, role permissiveRole) bool {
	next := parser.nextNonWhitespace(afterRun)
	if next >= len(parser.characters) {
		return true
	}
	nextCharacter := canonicalStructuralRune(parser.characters[next])
	if role == permissiveKey {
		return nextCharacter == ':' || nextCharacter == ',' || nextCharacter == '}' || nextCharacter == ']'
	}
	return nextCharacter == ',' || nextCharacter == '}' || nextCharacter == ']'
}

func (parser *permissiveParser) quoteCloses(at int, role permissiveRole) bool {
	next := parser.nextNonWhitespace(at + 1)
	if next >= len(parser.characters) {
		return true
	}
	nextCharacter := canonicalStructuralRune(parser.characters[next])
	if role == permissiveKey {
		if nextCharacter == ':' || nextCharacter == ',' || nextCharacter == '}' || nextCharacter == ']' {
			return true
		}
		if next > at+1 && parser.canStartRecoveredValue(nextCharacter) {
			return true
		}
		return next == at+1 && (nextCharacter == '"' || nextCharacter == '\'')
	}

	if nextCharacter == '}' || nextCharacter == ']' {
		return true
	}
	if nextCharacter != ',' {
		if next > at+1 && (nextCharacter == '"' || nextCharacter == '\'') {
			if parser.insideObject() && parser.looksLikeObjectKey(next) || parser.insideArray() {
				return true
			}
		}
		if next > at+1 && parser.junkFollowsString(next) {
			return true
		}
		return false
	}
	afterComma := parser.nextNonWhitespace(next + 1)
	if afterComma >= len(parser.characters) || canonicalStructuralRune(parser.characters[afterComma]) == '}' || canonicalStructuralRune(parser.characters[afterComma]) == ']' {
		return true
	}
	if parser.insideObject() {
		return parser.looksLikeObjectKey(afterComma)
	}
	return true
}

func (parser *permissiveParser) junkFollowsString(from int) bool {
	for index, end := from, parser.lookaheadEnd(from); index < end; index++ {
		switch canonicalStructuralRune(parser.characters[index]) {
		case '}', ']':
			return true
		case ',', ':':
			return false
		case '"', '\'':
			return false
		}
	}
	return false
}

func (parser *permissiveParser) looksLikeObjectKey(at int) bool {
	if at >= len(parser.characters) {
		return false
	}
	quote := parser.characters[at]
	if quote != '"' && quote != '\'' {
		return false
	}
	for at, end := at+1, parser.lookaheadEnd(at+1); at < end; at++ {
		if parser.characters[at] == '\\' {
			at++
			continue
		}
		if parser.characters[at] == quote {
			return parser.nextRuneIs(parser.nextNonWhitespace(at+1), ':')
		}
	}
	return false
}

func (parser *permissiveParser) consumePermissiveEscape(value *strings.Builder, role permissiveRole) {
	parser.position++
	if parser.atEnd() {
		value.WriteRune('\\')
		return
	}

	character := parser.current()
	parser.position++
	switch character {
	case '"', '\\', '/':
		value.WriteRune(character)
	case '\'':
		value.WriteRune('\'')
	case 'b':
		value.WriteRune('\b')
	case 'f':
		value.WriteRune('\f')
	case 'n':
		if role != permissiveKey {
			value.WriteRune('\n')
		}
	case 'r':
		if role != permissiveKey {
			value.WriteRune('\r')
		}
	case 't':
		value.WriteRune('\t')
	case 'u':
		if decoded, ok := parser.consumeUnicodeEscape(); ok {
			value.WriteRune(decoded)
		} else {
			value.WriteRune('u')
		}
	default:
		if character != utf8.RuneError {
			value.WriteRune(character)
		}
	}
}

func (parser *permissiveParser) consumeUnicodeEscape() (rune, bool) {
	if parser.position+4 > len(parser.characters) {
		return 0, false
	}
	first, ok := decodeHexRunes(parser.characters[parser.position : parser.position+4])
	if !ok {
		return 0, false
	}
	parser.position += 4
	if first >= 0xd800 && first <= 0xdbff {
		if parser.position+6 <= len(parser.characters) && parser.characters[parser.position] == '\\' && parser.characters[parser.position+1] == 'u' {
			second, secondOK := decodeHexRunes(parser.characters[parser.position+2 : parser.position+6])
			if secondOK && second >= 0xdc00 && second <= 0xdfff {
				parser.position += 6
				return utf16.DecodeRune(first, second), true
			}
		}
		return utf8.RuneError, true
	}
	if first >= 0xdc00 && first <= 0xdfff {
		return utf8.RuneError, true
	}
	return first, true
}

func decodeHexRunes(characters []rune) (rune, bool) {
	if len(characters) != 4 {
		return 0, false
	}
	value := 0
	for _, character := range characters {
		digit := strings.IndexRune("0123456789abcdef", unicode.ToLower(character))
		if digit < 0 {
			return 0, false
		}
		value = value*16 + digit
	}
	return rune(value), true
}

func (parser *permissiveParser) parseBareValue() recoveredValue {
	start := parser.position
	for !parser.atEnd() {
		character := parser.currentStructural()
		if character == ',' || character == '}' || character == ']' {
			break
		}
		if unicode.IsSpace(character) {
			prefix := cleanRecoveredText(parser.characters[start:parser.position])
			next := parser.nextNonWhitespace(parser.position)
			if next < len(parser.characters) {
				if isRecoveredPrimitive(prefix) && parser.canStartRecoveredValue(parser.characters[next]) {
					break
				}
				if parser.insideObject() && (parser.characters[next] == '"' || parser.characters[next] == '\'') && parser.looksLikeObjectKey(next) {
					break
				}
			}
		}
		parser.position++
	}

	text := cleanRecoveredText(parser.characters[start:parser.position])
	text = strings.TrimSpace(strings.TrimRight(text, "\"'"))
	if text == "" {
		return recoveredValue{}
	}
	switch strings.ToLower(text) {
	case "true":
		return recoveredValue{value: true, present: true}
	case "false":
		return recoveredValue{value: false, present: true}
	case "null":
		return recoveredValue{value: nil, present: true}
	}

	number := text
	if strings.HasPrefix(number, ".") {
		number = "0" + number
	} else if strings.HasPrefix(number, "-.") {
		number = "-0" + number[1:]
	}
	if isJSONNumber(number) {
		return recoveredValue{value: json.Number(number), present: true}
	}
	return recoveredValue{value: text, present: true}
}

func cleanRecoveredText(characters []rune) string {
	filtered := make([]rune, 0, len(characters))
	for _, character := range characters {
		if character != utf8.RuneError {
			filtered = append(filtered, character)
		}
	}
	return strings.TrimSpace(string(filtered))
}

func isJSONNumber(value string) bool {
	if value == "" {
		return false
	}
	if value[0] != '-' && (value[0] < '0' || value[0] > '9') {
		return false
	}
	return json.Valid([]byte(value))
}

func isRecoveredPrimitive(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "null":
		return true
	}
	if strings.HasPrefix(value, ".") {
		value = "0" + value
	} else if strings.HasPrefix(value, "-.") {
		value = "-0" + value[1:]
	}
	return isJSONNumber(value)
}

func (parser *permissiveParser) enter() error {
	if parser.depth >= parser.limits.MaxDepth {
		return newError(ErrLimitExceeded, parser.byteOffset(parser.position))
	}
	parser.depth++
	return nil
}

func (parser *permissiveParser) leave() {
	parser.depth--
}

func (parser *permissiveParser) skipWhitespace() {
	for !parser.atEnd() && unicode.IsSpace(parser.current()) {
		parser.position++
	}
}

func (parser *permissiveParser) skipWhitespaceAndCommas() {
	for !parser.atEnd() && (unicode.IsSpace(parser.current()) || parser.currentStructural() == ',') {
		parser.position++
	}
}

func (parser *permissiveParser) nextNonWhitespace(position int) int {
	if position < 0 {
		return 0
	}
	if position >= len(parser.nextSignificant) {
		return len(parser.characters)
	}
	return parser.nextSignificant[position]
}

func (parser *permissiveParser) lookaheadEnd(position int) int {
	end := position + maxPermissiveLookahead
	if end > len(parser.characters) {
		return len(parser.characters)
	}
	return end
}

func (parser *permissiveParser) byteOffset(position int) int {
	if position < 0 {
		return 0
	}
	if position >= len(parser.byteOffsets) {
		return parser.byteOffsets[len(parser.byteOffsets)-1]
	}
	return parser.byteOffsets[position]
}

func (parser *permissiveParser) nextRuneIs(position int, expected rune) bool {
	return position < len(parser.characters) && canonicalStructuralRune(parser.characters[position]) == expected
}

func (parser *permissiveParser) canStartRecoveredValue(character rune) bool {
	character = canonicalStructuralRune(character)
	return character == '{' || character == '[' || character == '"' || character == '\'' ||
		character == '-' || character == '.' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

func (parser *permissiveParser) insideObject() bool {
	return len(parser.containers) > 0 && parser.containers[len(parser.containers)-1] == '{'
}

func (parser *permissiveParser) insideArray() bool {
	return len(parser.containers) > 0 && parser.containers[len(parser.containers)-1] == '['
}

func (parser *permissiveParser) atEnd() bool {
	return parser.position >= len(parser.characters)
}

func (parser *permissiveParser) current() rune {
	return parser.characters[parser.position]
}

func (parser *permissiveParser) currentStructural() rune {
	return canonicalStructuralRune(parser.current())
}

func isStringBoundary(character rune, role permissiveRole) bool {
	character = canonicalStructuralRune(character)
	if role == permissiveKey {
		return character == ':' || character == ',' || character == '}' || character == ']'
	}
	return character == ',' || character == '}' || character == ']'
}
