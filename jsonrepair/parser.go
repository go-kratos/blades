package jsonrepair

type stringRole uint8

const (
	stringValue stringRole = iota
	stringKey
)

type parser struct {
	input    []byte
	position int
	stack    []byte
	edits    []Edit
	inserted int
	limits   Limits
}

func (parser *parser) parse() error {
	parser.skipWhitespace()
	if parser.atEnd() {
		return newError(ErrUnrepairable, parser.position)
	}
	if err := parser.parseValue(); err != nil {
		return err
	}
	parser.skipWhitespace()
	if !parser.atEnd() {
		return newError(ErrUnrepairable, parser.position)
	}
	return nil
}

func (parser *parser) parseValue() error {
	parser.skipWhitespace()
	if parser.atEnd() {
		return newError(ErrUnrepairable, parser.position)
	}

	switch parser.input[parser.position] {
	case '{':
		return parser.parseObject()
	case '[':
		return parser.parseArray()
	case '"':
		return parser.parseString(stringValue)
	case 't':
		return parser.parseLiteral("true")
	case 'f':
		return parser.parseLiteral("false")
	case 'n':
		return parser.parseLiteral("null")
	case '-':
		return parser.parseNumber()
	default:
		if isDigit(parser.input[parser.position]) {
			return parser.parseNumber()
		}
		return newError(ErrUnrepairable, parser.position)
	}
}

func (parser *parser) parseObject() error {
	if err := parser.push('}'); err != nil {
		return err
	}
	parser.position++
	parser.skipWhitespace()

	if parser.atEnd() {
		return parser.insertContainerClose('}')
	}
	if parser.input[parser.position] == '}' {
		parser.position++
		parser.pop()
		return nil
	}
	if parser.isAncestorCloser(parser.input[parser.position]) {
		return parser.insertContainerClose('}')
	}

	for {
		if parser.atEnd() || parser.input[parser.position] != '"' {
			return newError(ErrUnrepairable, parser.position)
		}
		if err := parser.parseString(stringKey); err != nil {
			return err
		}

		beforeWhitespace := parser.position
		parser.skipWhitespace()
		if parser.atEnd() {
			return newError(ErrUnrepairable, parser.position)
		}
		if parser.input[parser.position] == ':' {
			parser.position++
		} else if parser.position > beforeWhitespace && canStartValue(parser.input[parser.position]) {
			if err := parser.insert(EditInsertColon, parser.position, ":"); err != nil {
				return err
			}
		} else {
			return newError(ErrUnrepairable, parser.position)
		}

		if err := parser.parseValue(); err != nil {
			return err
		}
		beforeWhitespace = parser.position
		parser.skipWhitespace()
		if parser.atEnd() {
			return parser.insertContainerClose('}')
		}

		switch parser.input[parser.position] {
		case '}':
			parser.position++
			parser.pop()
			return nil
		case ',':
			parser.position++
			parser.skipWhitespace()
			if parser.atEnd() || parser.input[parser.position] == '}' || parser.isAncestorCloser(parser.input[parser.position]) {
				return newError(ErrUnrepairable, parser.position)
			}
		case '"':
			if parser.position == beforeWhitespace {
				return newError(ErrUnrepairable, parser.position)
			}
			if err := parser.insert(EditInsertComma, parser.position, ","); err != nil {
				return err
			}
		default:
			if parser.isAncestorCloser(parser.input[parser.position]) {
				return parser.insertContainerClose('}')
			}
			return newError(ErrUnrepairable, parser.position)
		}
	}
}

func (parser *parser) parseArray() error {
	if err := parser.push(']'); err != nil {
		return err
	}
	parser.position++
	parser.skipWhitespace()

	if parser.atEnd() {
		return parser.insertContainerClose(']')
	}
	if parser.input[parser.position] == ']' {
		parser.position++
		parser.pop()
		return nil
	}
	if parser.isAncestorCloser(parser.input[parser.position]) {
		return parser.insertContainerClose(']')
	}

	for {
		if err := parser.parseValue(); err != nil {
			return err
		}
		beforeWhitespace := parser.position
		parser.skipWhitespace()
		if parser.atEnd() {
			return parser.insertContainerClose(']')
		}

		switch parser.input[parser.position] {
		case ']':
			parser.position++
			parser.pop()
			return nil
		case ',':
			parser.position++
			parser.skipWhitespace()
			if parser.atEnd() || parser.input[parser.position] == ']' || parser.isAncestorCloser(parser.input[parser.position]) {
				return newError(ErrUnrepairable, parser.position)
			}
		default:
			if parser.isAncestorCloser(parser.input[parser.position]) {
				return parser.insertContainerClose(']')
			}
			if parser.position > beforeWhitespace && canStartValue(parser.input[parser.position]) {
				if err := parser.insert(EditInsertComma, parser.position, ","); err != nil {
					return err
				}
				continue
			}
			return newError(ErrUnrepairable, parser.position)
		}
	}
}

func (parser *parser) parseString(role stringRole) error {
	parser.position++
	for !parser.atEnd() {
		character := parser.input[parser.position]
		switch {
		case character == '"':
			closes, err := parser.quoteCloses(parser.position, role)
			if err != nil {
				return err
			}
			if closes {
				parser.position++
				return nil
			}
			if err := parser.insert(EditEscapeQuote, parser.position, "\\"); err != nil {
				return err
			}
			parser.position++
		case character == '\\':
			if err := parser.consumeEscape(); err != nil {
				return err
			}
		case character < 0x20:
			return newError(ErrUnrepairable, parser.position)
		default:
			parser.position++
		}
	}
	return parser.insert(EditCloseString, parser.position, "\"")
}

func (parser *parser) consumeEscape() error {
	start := parser.position
	parser.position++
	if parser.atEnd() {
		return newError(ErrUnrepairable, start)
	}

	switch parser.input[parser.position] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		parser.position++
		return nil
	case 'u':
		parser.position++
		for range 4 {
			if parser.atEnd() || !isHex(parser.input[parser.position]) {
				return newError(ErrUnrepairable, start)
			}
			parser.position++
		}
		return nil
	default:
		return newError(ErrUnrepairable, start)
	}
}

func (parser *parser) quoteCloses(at int, role stringRole) (bool, error) {
	next := parser.nextNonWhitespace(at + 1)
	hasWhitespace := next > at+1
	if role == stringKey {
		if next == len(parser.input) {
			return true, nil
		}
		nextCharacter := parser.input[next]
		if nextCharacter == ':' {
			return true, nil
		}
		if hasWhitespace && nextCharacter == '"' {
			return false, newError(ErrAmbiguous, at)
		}
		if hasWhitespace && canStartValue(nextCharacter) {
			return true, nil
		}
		if nextCharacter == '}' || nextCharacter == ']' || nextCharacter == ',' {
			return true, nil
		}
		return false, nil
	}

	if next == len(parser.input) {
		return true, nil
	}
	nextCharacter := parser.input[next]
	if parser.isCurrentOrAncestorCloser(nextCharacter) {
		return true, nil
	}
	if nextCharacter == ',' {
		afterComma := parser.nextNonWhitespace(next + 1)
		if afterComma == len(parser.input) || parser.isCurrentOrAncestorCloser(parser.input[afterComma]) {
			return false, newError(ErrUnrepairable, next)
		}
		if parser.commaCanSeparate(parser.input[afterComma]) {
			return true, nil
		}
		return false, nil
	}
	if hasWhitespace && parser.canFollowMissingComma(nextCharacter) {
		return false, newError(ErrAmbiguous, at)
	}
	return false, nil
}

func (parser *parser) commaCanSeparate(next byte) bool {
	if len(parser.stack) == 0 {
		return false
	}
	switch parser.stack[len(parser.stack)-1] {
	case '}':
		return next == '"'
	case ']':
		return canStartValue(next)
	default:
		return false
	}
}

func (parser *parser) canFollowMissingComma(next byte) bool {
	if len(parser.stack) == 0 {
		return false
	}
	switch parser.stack[len(parser.stack)-1] {
	case '}':
		return next == '"'
	case ']':
		return canStartValue(next)
	default:
		return false
	}
}

func (parser *parser) parseLiteral(literal string) error {
	start := parser.position
	if len(parser.input)-start < len(literal) {
		return newError(ErrUnrepairable, start)
	}
	for index := range len(literal) {
		if parser.input[start+index] != literal[index] {
			return newError(ErrUnrepairable, start)
		}
	}
	parser.position += len(literal)
	if !parser.atEnd() && !isValueBoundary(parser.input[parser.position]) {
		return newError(ErrUnrepairable, parser.position)
	}
	return nil
}

func (parser *parser) parseNumber() error {
	start := parser.position
	if parser.input[parser.position] == '-' {
		parser.position++
		if parser.atEnd() {
			return newError(ErrUnrepairable, start)
		}
	}

	if parser.input[parser.position] == '0' {
		parser.position++
	} else if isNonZeroDigit(parser.input[parser.position]) {
		for !parser.atEnd() && isDigit(parser.input[parser.position]) {
			parser.position++
		}
	} else {
		return newError(ErrUnrepairable, start)
	}

	if !parser.atEnd() && parser.input[parser.position] == '.' {
		parser.position++
		if parser.atEnd() || !isDigit(parser.input[parser.position]) {
			return newError(ErrUnrepairable, start)
		}
		for !parser.atEnd() && isDigit(parser.input[parser.position]) {
			parser.position++
		}
	}

	if !parser.atEnd() && (parser.input[parser.position] == 'e' || parser.input[parser.position] == 'E') {
		parser.position++
		if !parser.atEnd() && (parser.input[parser.position] == '+' || parser.input[parser.position] == '-') {
			parser.position++
		}
		if parser.atEnd() || !isDigit(parser.input[parser.position]) {
			return newError(ErrUnrepairable, start)
		}
		for !parser.atEnd() && isDigit(parser.input[parser.position]) {
			parser.position++
		}
	}

	if !parser.atEnd() && !isValueBoundary(parser.input[parser.position]) {
		return newError(ErrUnrepairable, parser.position)
	}
	return nil
}

func (parser *parser) push(closer byte) error {
	if len(parser.stack) >= parser.limits.MaxDepth {
		return newError(ErrLimitExceeded, parser.position)
	}
	parser.stack = append(parser.stack, closer)
	return nil
}

func (parser *parser) pop() {
	parser.stack = parser.stack[:len(parser.stack)-1]
}

func (parser *parser) insertContainerClose(closer byte) error {
	kind := EditCloseArray
	if closer == '}' {
		kind = EditCloseObject
	}
	if err := parser.insert(kind, parser.position, string(closer)); err != nil {
		return err
	}
	parser.pop()
	return nil
}

func (parser *parser) insert(kind EditKind, at int, replacement string) error {
	if len(parser.edits) >= parser.limits.MaxEdits {
		return newError(ErrLimitExceeded, at)
	}
	if len(parser.input)+parser.inserted+len(replacement) > parser.limits.MaxOutputBytes {
		return newError(ErrLimitExceeded, at)
	}
	parser.edits = append(parser.edits, Edit{
		Kind:        kind,
		Start:       at,
		End:         at,
		Replacement: replacement,
	})
	parser.inserted += len(replacement)
	return nil
}

func (parser *parser) skipWhitespace() {
	for !parser.atEnd() && isWhitespace(parser.input[parser.position]) {
		parser.position++
	}
}

func (parser *parser) nextNonWhitespace(position int) int {
	for position < len(parser.input) && isWhitespace(parser.input[position]) {
		position++
	}
	return position
}

func (parser *parser) atEnd() bool {
	return parser.position >= len(parser.input)
}

func (parser *parser) isCurrentOrAncestorCloser(character byte) bool {
	for index := len(parser.stack) - 1; index >= 0; index-- {
		if parser.stack[index] == character {
			return true
		}
	}
	return false
}

func (parser *parser) isAncestorCloser(character byte) bool {
	for index := len(parser.stack) - 2; index >= 0; index-- {
		if parser.stack[index] == character {
			return true
		}
	}
	return false
}

func canStartValue(character byte) bool {
	return character == '{' || character == '[' || character == '"' ||
		character == 't' || character == 'f' || character == 'n' ||
		character == '-' || isDigit(character)
}

func isValueBoundary(character byte) bool {
	return isWhitespace(character) || character == ',' || character == '}' || character == ']'
}

func isWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func isNonZeroDigit(character byte) bool {
	return character >= '1' && character <= '9'
}

func isHex(character byte) bool {
	return isDigit(character) || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F'
}
