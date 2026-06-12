package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

func MergeRenderedContent(strategy FileMergeStrategy, path string, existing, desired []byte) ([]byte, error) {
	switch strategy {
	case FileMergeReplace:
		return desired, nil
	case FileMergeCodexConfig:
		return mergeTOMLTable(path, existing, desired, "mcp_servers")
	case FileMergeClaudeSettings:
		return mergeJSONFields(path, existing, desired, "hooks")
	case FileMergeClaudeMCP:
		return mergeJSONMapField(path, existing, desired, "mcpServers")
	default:
		return nil, fmt.Errorf("unsupported merge strategy %q for %s", strategy, path)
	}
}

func mergeTOMLTable(path string, existing, desired []byte, tableName string) ([]byte, error) {
	existingDoc, err := parseTOMLDocument(path, existing)
	if err != nil {
		return nil, err
	}
	desiredDoc, err := parseTOMLDocument(path, desired)
	if err != nil {
		return nil, fmt.Errorf("generated content for %s is invalid: %w", path, err)
	}
	desiredTable, ok, err := objectValue(desiredDoc, tableName)
	if err != nil {
		return nil, fmt.Errorf("generated content for %s is invalid: %w", path, err)
	}
	if !ok {
		return renderTOML(existingDoc)
	}
	existingTable, ok, err := objectValue(existingDoc, tableName)
	if err != nil {
		return nil, fmt.Errorf("cannot merge %s: %w", path, err)
	}
	if !ok {
		existingTable = map[string]any{}
	}
	for name, value := range desiredTable {
		existingTable[name] = value
	}
	existingDoc[tableName] = existingTable
	return renderTOML(existingDoc)
}

func mergeJSONMapField(path string, existing, desired []byte, field string) ([]byte, error) {
	existingDoc, err := parseJSONDocument(path, existing)
	if err != nil {
		return nil, err
	}
	originalDoc, err := parseJSONDocument(path, existing)
	if err != nil {
		return nil, err
	}
	desiredDoc, err := parseJSONDocument(path, desired)
	if err != nil {
		return nil, fmt.Errorf("generated content for %s is invalid: %w", path, err)
	}
	desiredMap, ok, err := objectValue(desiredDoc, field)
	if err != nil {
		return nil, fmt.Errorf("generated content for %s is invalid: %w", path, err)
	}
	if !ok {
		return renderJSON(existingDoc)
	}
	existingMap, ok, err := objectValue(existingDoc, field)
	if err != nil {
		return nil, fmt.Errorf("cannot merge %s: %w", path, err)
	}
	if !ok {
		existingMap = map[string]any{}
	}
	for name, value := range desiredMap {
		existingMap[name] = value
	}
	existingDoc[field] = existingMap
	if reflect.DeepEqual(existingDoc, originalDoc) {
		return existing, nil
	}
	if data, ok := mergeExistingJSONObjectMembers(existing, field, desiredMap, false); ok {
		return data, nil
	}
	return renderJSON(existingDoc)
}

func mergeJSONFields(path string, existing, desired []byte, fields ...string) ([]byte, error) {
	existingDoc, err := parseJSONDocument(path, existing)
	if err != nil {
		return nil, err
	}
	originalDoc, err := parseJSONDocument(path, existing)
	if err != nil {
		return nil, err
	}
	desiredDoc, err := parseJSONDocument(path, desired)
	if err != nil {
		return nil, fmt.Errorf("generated content for %s is invalid: %w", path, err)
	}
	for _, field := range fields {
		if value, ok := desiredDoc[field]; ok {
			existingDoc[field] = value
		}
	}
	if reflect.DeepEqual(existingDoc, originalDoc) {
		return existing, nil
	}
	if data, ok := mergeExistingJSONFields(existing, desiredDoc, fields); ok {
		return data, nil
	}
	return renderJSON(existingDoc)
}

type jsonValueRange struct {
	keyStart int
	start    int
	end      int
}

type jsonReplacement struct {
	start int
	end   int
	data  []byte
}

func mergeExistingJSONFields(existing []byte, desiredDoc map[string]any, fields []string) ([]byte, bool) {
	out := existing
	for _, field := range fields {
		desiredValue, ok := desiredDoc[field]
		if !ok {
			continue
		}
		desiredObject, desiredIsObject := desiredValue.(map[string]any)
		if desiredIsObject {
			if data, ok := mergeExistingJSONObjectMembers(out, field, desiredObject, true); ok {
				out = data
				continue
			}
		}
		data, ok := replaceExistingJSONField(out, field, desiredValue)
		if !ok {
			return nil, false
		}
		out = data
	}
	return out, true
}

func mergeExistingJSONObjectMembers(existing []byte, field string, desiredObject map[string]any, replaceExact bool) ([]byte, bool) {
	existingDoc, err := parseJSONDocument("", existing)
	if err != nil {
		return nil, false
	}
	existingValue, ok := existingDoc[field]
	if !ok {
		return nil, false
	}
	existingObject, ok := existingValue.(map[string]any)
	if !ok {
		return nil, false
	}
	if replaceExact && len(existingObject) != len(desiredObject) {
		return nil, false
	}
	for name := range desiredObject {
		if _, ok := existingObject[name]; !ok {
			return nil, false
		}
	}
	topRanges, err := objectValueRanges(existing)
	if err != nil {
		return nil, false
	}
	fieldRange, ok := topRanges[field]
	if !ok {
		return nil, false
	}
	childRanges, err := objectValueRanges(existing[fieldRange.start:fieldRange.end])
	if err != nil {
		return nil, false
	}
	var replacements []jsonReplacement
	for name, desiredValue := range desiredObject {
		if reflect.DeepEqual(existingObject[name], desiredValue) {
			continue
		}
		childRange, ok := childRanges[name]
		if !ok {
			return nil, false
		}
		abs := jsonValueRange{
			keyStart: fieldRange.start + childRange.keyStart,
			start:    fieldRange.start + childRange.start,
			end:      fieldRange.start + childRange.end,
		}
		data, ok := mergeJSONValuePreservingLayout(existing[abs.start:abs.end], existingObject[name], desiredValue)
		if !ok {
			var err error
			data, err = renderJSONValueForReplacement(desiredValue, existing, abs.keyStart)
			if err != nil {
				return nil, false
			}
		}
		replacements = append(replacements, jsonReplacement{start: abs.start, end: abs.end, data: data})
	}
	if len(replacements) == 0 {
		return existing, true
	}
	return applyJSONReplacements(existing, replacements), true
}

func replaceExistingJSONField(existing []byte, field string, value any) ([]byte, bool) {
	ranges, err := objectValueRanges(existing)
	if err != nil {
		return nil, false
	}
	item, ok := ranges[field]
	if !ok {
		return nil, false
	}
	data, err := renderJSONValueForReplacement(value, existing, item.keyStart)
	if err != nil {
		return nil, false
	}
	out := make([]byte, 0, len(existing)-(item.end-item.start)+len(data))
	out = append(out, existing[:item.start]...)
	out = append(out, data...)
	out = append(out, existing[item.end:]...)
	return out, true
}

func mergeJSONValuePreservingLayout(existing []byte, existingValue, desiredValue any) ([]byte, bool) {
	if reflect.DeepEqual(existingValue, desiredValue) {
		return existing, true
	}
	switch existingTyped := existingValue.(type) {
	case map[string]any:
		desiredTyped, ok := desiredValue.(map[string]any)
		if !ok || len(existingTyped) != len(desiredTyped) {
			return nil, false
		}
		for name := range desiredTyped {
			if _, ok := existingTyped[name]; !ok {
				return nil, false
			}
		}
		ranges, err := objectValueRanges(existing)
		if err != nil {
			return nil, false
		}
		var replacements []jsonReplacement
		for name, desiredChild := range desiredTyped {
			existingChild := existingTyped[name]
			if reflect.DeepEqual(existingChild, desiredChild) {
				continue
			}
			childRange, ok := ranges[name]
			if !ok {
				return nil, false
			}
			data, ok := mergeJSONValuePreservingLayout(existing[childRange.start:childRange.end], existingChild, desiredChild)
			if !ok {
				var err error
				data, err = renderJSONValueForReplacement(desiredChild, existing, childRange.keyStart)
				if err != nil {
					return nil, false
				}
			}
			replacements = append(replacements, jsonReplacement{
				start: childRange.start,
				end:   childRange.end,
				data:  data,
			})
		}
		if len(replacements) == 0 {
			return existing, true
		}
		return applyJSONReplacements(existing, replacements), true
	case []any:
		desiredTyped, ok := desiredValue.([]any)
		if !ok || len(existingTyped) != len(desiredTyped) {
			return nil, false
		}
		ranges, err := arrayValueRanges(existing)
		if err != nil || len(ranges) != len(existingTyped) {
			return nil, false
		}
		var replacements []jsonReplacement
		for i, desiredChild := range desiredTyped {
			existingChild := existingTyped[i]
			if reflect.DeepEqual(existingChild, desiredChild) {
				continue
			}
			childRange := ranges[i]
			data, ok := mergeJSONValuePreservingLayout(existing[childRange.start:childRange.end], existingChild, desiredChild)
			if !ok {
				var err error
				data, err = renderJSONValueForReplacement(desiredChild, existing, childRange.start)
				if err != nil {
					return nil, false
				}
			}
			replacements = append(replacements, jsonReplacement{
				start: childRange.start,
				end:   childRange.end,
				data:  data,
			})
		}
		if len(replacements) == 0 {
			return existing, true
		}
		return applyJSONReplacements(existing, replacements), true
	default:
		return nil, false
	}
}

func applyJSONReplacements(existing []byte, replacements []jsonReplacement) []byte {
	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].start < replacements[j].start
	})
	out := append([]byte(nil), existing...)
	for i := len(replacements) - 1; i >= 0; i-- {
		item := replacements[i]
		next := make([]byte, 0, len(out)-(item.end-item.start)+len(item.data))
		next = append(next, out[:item.start]...)
		next = append(next, item.data...)
		next = append(next, out[item.end:]...)
		out = next
	}
	return out
}

func renderJSONValueForReplacement(value any, existing []byte, linePos int) ([]byte, error) {
	data, err := renderJSON(value)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	indent := lineIndent(existing, linePos)
	lines := bytes.Split(data, []byte("\n"))
	for i := 1; i < len(lines); i++ {
		lines[i] = append(append([]byte(nil), indent...), lines[i]...)
	}
	return bytes.Join(lines, []byte("\n")), nil
}

func objectValueRanges(data []byte) (map[string]jsonValueRange, error) {
	out := map[string]jsonValueRange{}
	pos := skipJSONSpace(data, 0)
	if pos >= len(data) || data[pos] != '{' {
		return nil, fmt.Errorf("json value is not an object")
	}
	pos++
	for {
		pos = skipJSONSpace(data, pos)
		if pos >= len(data) {
			return nil, fmt.Errorf("unterminated json object")
		}
		if data[pos] == '}' {
			return out, nil
		}
		keyStart := pos
		keyEnd, err := scanJSONString(data, pos)
		if err != nil {
			return nil, err
		}
		key, err := strconv.Unquote(string(data[keyStart:keyEnd]))
		if err != nil {
			return nil, err
		}
		pos = skipJSONSpace(data, keyEnd)
		if pos >= len(data) || data[pos] != ':' {
			return nil, fmt.Errorf("json object key %q missing colon", key)
		}
		pos = skipJSONSpace(data, pos+1)
		valueStart := pos
		valueEnd, err := scanJSONValue(data, valueStart)
		if err != nil {
			return nil, err
		}
		out[key] = jsonValueRange{keyStart: keyStart, start: valueStart, end: valueEnd}
		pos = skipJSONSpace(data, valueEnd)
		if pos >= len(data) {
			return nil, fmt.Errorf("unterminated json object")
		}
		switch data[pos] {
		case ',':
			pos++
		case '}':
			return out, nil
		default:
			return nil, fmt.Errorf("json object key %q has invalid separator", key)
		}
	}
}

func arrayValueRanges(data []byte) ([]jsonValueRange, error) {
	var out []jsonValueRange
	pos := skipJSONSpace(data, 0)
	if pos >= len(data) || data[pos] != '[' {
		return nil, fmt.Errorf("json value is not an array")
	}
	pos++
	for {
		pos = skipJSONSpace(data, pos)
		if pos >= len(data) {
			return nil, fmt.Errorf("unterminated json array")
		}
		if data[pos] == ']' {
			return out, nil
		}
		valueStart := pos
		valueEnd, err := scanJSONValue(data, valueStart)
		if err != nil {
			return nil, err
		}
		out = append(out, jsonValueRange{keyStart: valueStart, start: valueStart, end: valueEnd})
		pos = skipJSONSpace(data, valueEnd)
		if pos >= len(data) {
			return nil, fmt.Errorf("unterminated json array")
		}
		switch data[pos] {
		case ',':
			pos++
		case ']':
			return out, nil
		default:
			return nil, fmt.Errorf("json array has invalid separator")
		}
	}
}

func scanJSONValue(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, fmt.Errorf("missing json value")
	}
	switch data[pos] {
	case '"':
		return scanJSONString(data, pos)
	case '{', '[':
		return scanJSONComposite(data, pos)
	default:
		end := pos
		for end < len(data) && !isJSONSpace(data[end]) && data[end] != ',' && data[end] != '}' && data[end] != ']' {
			end++
		}
		if end == pos {
			return 0, fmt.Errorf("invalid json value")
		}
		return end, nil
	}
}

func scanJSONString(data []byte, pos int) (int, error) {
	if pos >= len(data) || data[pos] != '"' {
		return 0, fmt.Errorf("json string expected")
	}
	pos++
	for pos < len(data) {
		switch data[pos] {
		case '\\':
			pos += 2
		case '"':
			return pos + 1, nil
		default:
			pos++
		}
	}
	return 0, fmt.Errorf("unterminated json string")
}

func scanJSONComposite(data []byte, pos int) (int, error) {
	var stack []byte
	for pos < len(data) {
		switch data[pos] {
		case '"':
			end, err := scanJSONString(data, pos)
			if err != nil {
				return 0, err
			}
			pos = end
			continue
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || data[pos] != stack[len(stack)-1] {
				return 0, fmt.Errorf("mismatched json delimiter")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return pos + 1, nil
			}
		}
		pos++
	}
	return 0, fmt.Errorf("unterminated json composite")
}

func skipJSONSpace(data []byte, pos int) int {
	for pos < len(data) && isJSONSpace(data[pos]) {
		pos++
	}
	return pos
}

func isJSONSpace(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t'
}

func lineIndent(data []byte, pos int) []byte {
	start := pos
	for start > 0 && data[start-1] != '\n' && data[start-1] != '\r' {
		start--
	}
	end := start
	for end < pos && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return data[start:end]
}

func parseTOMLDocument(path string, data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := tomlUnmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("cannot parse %s for merge: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func parseJSONDocument(path string, data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("cannot parse %s for merge: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func objectValue(doc map[string]any, name string) (map[string]any, bool, error) {
	value, ok := doc[name]
	if !ok {
		return nil, false, nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%s is not an object", name)
	}
	return typed, true, nil
}
