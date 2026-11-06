package diagnostics

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const redactedValue = "[redacted]"

var (
	byteValuePattern  = regexp.MustCompile(`(?i)^\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*(B|KB|MB|GB|KIB|MIB|GIB)?\b`)
	macPattern        = regexp.MustCompile(`(?i)^(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}$`)
	uptimePattern     = regexp.MustCompile(`(?i)^\s*(?:(\d+)\s+days?,\s*)?(\d{1,2}):(\d{2}):(\d{2})\s*$`)
	jsonSecretPattern = regexp.MustCompile(
		`(?i)("(?:[^"\\]|\\.)*(?:password|passwd|passphrase|secret|token|authorization|credential|cookie|session)(?:[^"\\]|\\.)*"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)`,
	)
	jsonShortSecretPattern = regexp.MustCompile(
		`(?i)("(?:pass|pwd|psk|wep[_-]?key|(?:[^"\\]|\\.)*[_-](?:pass|pwd|psk|wep[_-]?key))"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)`,
	)
)

// Parse accepts the actual ASA plaintext wire shape as well as JSON variants
// used by development doubles. A malformed optional field never invalidates
// the rest of a response: the document retains raw content for public sources
// and records warnings alongside every successfully parsed field. Sensitive
// projection sources are redacted before any raw value is retained.
func Parse(endpoint string, data []byte, fetchedAt time.Time) Document {
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	} else {
		fetchedAt = fetchedAt.UTC()
	}

	raw := strings.ToValidUTF8(string(data), "�")
	sensitive := IsSensitiveEndpoint(endpoint)
	if sensitive {
		// The audited firmware's station/AP configuration routes include a
		// plaintext `pass:` line. Remove credentials before any Document or
		// Field retains the response, not merely at JSON serialization time.
		raw = sanitizeSensitivePayload(raw)
	}
	document := Document{
		Endpoint:  strings.TrimSpace(endpoint),
		Format:    FormatUnknown,
		FetchedAt: fetchedAt,
		Bytes:     len(data),
		Raw:       raw,
		SafeRaw:   redactText(raw),
	}
	if !utf8.Valid(data) {
		document.Warnings = append(document.Warnings, "response contained invalid UTF-8")
	}
	if strings.ContainsRune(raw, '\x00') {
		document.Warnings = append(document.Warnings, "response contained NUL bytes")
	}
	if sensitive {
		document.Warnings = append(
			document.Warnings,
			"credential values were redacted before caching",
		)
	}
	if _, ok := LookupEndpoint(document.Endpoint); !ok {
		document.Warnings = append(document.Warnings, "endpoint is not in the PCControllerDesktop ESP diagnostics registry")
	}

	parseInput := strings.TrimSpace(strings.TrimRight(
		strings.TrimPrefix(raw, "\ufeff"),
		"\x00",
	))
	if parseInput == "" {
		document.Format = endpointFormatHint(document.Endpoint, FormatText)
		document.SafeRaw = strings.ReplaceAll(document.SafeRaw, "\x00", "␀")
		document.Warnings = append(document.Warnings, "endpoint returned an empty response")
		return document
	}

	if parseInput[0] == '{' || parseInput[0] == '[' {
		value, err := decodeSingleJSON([]byte(parseInput))
		if err == nil {
			document.Format = FormatJSON
			flattenJSON(document.Endpoint, "", value, &document.Fields)
			redacted := redactJSONValue("", value)
			if formatted, marshalErr := json.MarshalIndent(redacted, "", "  "); marshalErr == nil {
				document.SafeRaw = string(formatted)
			}
			normalizeDocumentFields(&document)
			return document
		}
		document.Warnings = append(document.Warnings, "JSON-looking response was malformed: "+err.Error())
	}

	if parseInput[0] == '<' || document.Endpoint == EndpointDescription {
		fields, err := parseXMLFields(document.Endpoint, parseInput)
		if err == nil {
			document.Format = FormatXML
			document.Fields = fields
			normalizeDocumentFields(&document)
			return document
		}
		document.Warnings = append(document.Warnings, "XML-looking response was malformed: "+err.Error())
	}

	document.Format = FormatText
	document.Fields = parseTextFields(document.Endpoint, parseInput, &document.Warnings)
	document.SafeRaw = redactText(raw)
	normalizeDocumentFields(&document)
	return document
}

func endpointFormatHint(endpoint string, fallback Format) Format {
	if spec, ok := LookupEndpoint(endpoint); ok {
		return spec.Hint
	}
	return fallback
}

func decodeSingleJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return value, nil
}

func flattenJSON(endpoint, prefix string, value any, fields *[]Field) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			childPrefix := key
			if prefix != "" {
				childPrefix = prefix + "." + key
			}
			flattenJSON(endpoint, childPrefix, typed[key], fields)
		}
	case []any:
		if len(typed) == 0 {
			key := prefix
			if key == "" {
				key = "value"
			}
			*fields = append(*fields, classifyField(endpoint, key, "[]"))
			return
		}
		for index, child := range typed {
			childPrefix := fmt.Sprintf("%s[%d]", prefix, index)
			if prefix == "" {
				childPrefix = fmt.Sprintf("value[%d]", index)
			}
			flattenJSON(endpoint, childPrefix, child, fields)
		}
	default:
		key := prefix
		if key == "" {
			key = "value"
		}
		field := classifyField(endpoint, key, jsonScalarString(typed))
		switch scalar := typed.(type) {
		case bool:
			if field.Kind == ValueString {
				field.Kind = ValueBoolean
			}
			value := scalar
			field.Boolean = &value
		case json.Number:
			if integer, err := scalar.Int64(); err == nil {
				if field.Kind == ValueString {
					field.Kind = ValueInteger
				}
				if field.Integer == nil {
					field.Integer = int64Pointer(integer)
				}
			} else if number, err := scalar.Float64(); err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
				if field.Kind == ValueString {
					field.Kind = ValueNumber
				}
				if field.Number == nil {
					field.Number = float64Pointer(number)
				}
			}
		}
		*fields = append(*fields, field)
	}
}

func jsonScalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func parseTextFields(endpoint, input string, warnings *[]string) []Field {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	lines := strings.Split(input, "\n")
	fields := make([]Field, 0, len(lines))
	unkeyed := 0
	for _, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine)
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "fail") {
			*warnings = append(*warnings, "firmware reported failure")
		}
		if key, value, ok := splitKeyValueLine(line); ok {
			fields = append(fields, classifyField(endpoint, key, value))
			continue
		}

		unkeyed++
		key := fmt.Sprintf("line_%d", unkeyed)
		switch endpoint {
		case EndpointWiFiSignal:
			if unkeyed == 1 {
				key = "rssi"
			} else if unkeyed == 2 {
				key = "quality"
			}
		case EndpointWiFiCountry:
			if unkeyed == 1 {
				key = "country"
			}
		}
		fields = append(fields, classifyField(endpoint, key, line))
	}
	return fields
}

func splitKeyValueLine(line string) (key, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	equals := strings.IndexByte(line, '=')
	separator := -1
	switch {
	case colon >= 0 && equals >= 0:
		separator = min(colon, equals)
	case colon >= 0:
		separator = colon
	case equals >= 0:
		separator = equals
	default:
		return "", "", false
	}
	key = strings.TrimSpace(line[:separator])
	if !plausibleTextKey(key) {
		return "", "", false
	}
	return key, strings.TrimSpace(line[separator+1:]), true
}

func plausibleTextKey(key string) bool {
	if key == "" || len(key) > 80 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(key)
	if !unicode.IsLetter(first) {
		return false
	}
	for _, character := range key {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			unicode.IsSpace(character) || strings.ContainsRune("._-/()", character) {
			continue
		}
		return false
	}
	return true
}

func parseXMLFields(endpoint, input string) ([]Field, error) {
	decoder := xml.NewDecoder(strings.NewReader(input))
	stack := make([]string, 0, 12)
	fields := make([]Field, 0, 12)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, typed.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			value := strings.TrimSpace(string(typed))
			if value == "" || len(stack) == 0 {
				continue
			}
			name := stack[len(stack)-1]
			if !isUsefulDescriptionTag(name) {
				continue
			}
			fields = append(fields, classifyField(endpoint, name, value))
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no recognized device description fields")
	}
	return fields, nil
}

func isUsefulDescriptionTag(tag string) bool {
	switch canonicalKey(tag) {
	case "friendly_name", "model_name", "model_number", "manufacturer",
		"manufacturer_url", "serial_number", "udn", "device_type",
		"presentation_url":
		return true
	default:
		return false
	}
}

func normalizeDocumentFields(document *Document) {
	if document == nil {
		return
	}
	seen := make(map[string]int, len(document.Fields))
	for index := range document.Fields {
		field := &document.Fields[index]
		if field.Key == "" {
			field.Key = fmt.Sprintf("field_%d", index+1)
		}
		seen[field.Key]++
		if seen[field.Key] > 1 {
			field.Key = fmt.Sprintf("%s_%d", field.Key, seen[field.Key])
		}
	}
	document.SafeRaw = strings.ReplaceAll(
		strings.ToValidUTF8(document.SafeRaw, "�"),
		"\x00",
		"␀",
	)
}

func classifyField(endpoint, sourceKey, raw string) Field {
	key := canonicalKey(sourceKey)
	if key == "" {
		key = "value"
	}
	field := Field{
		Key:     key,
		Label:   humanizeKey(sourceKey),
		Raw:     raw,
		Display: strings.TrimSpace(raw),
		Kind:    ValueString,
	}
	if field.Label == "" {
		field.Label = humanizeKey(key)
	}
	if isSensitiveKey(key) {
		field.Display = redactedValue
		field.Kind = ValueRedacted
		field.Redacted = true
		return field
	}

	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	switch {
	case isSignalKey(key):
		if integer, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(lower, "dbm")), 10, 64); err == nil {
			field.Kind = ValueSignal
			field.Unit = "dBm"
			field.Integer = int64Pointer(integer)
		}
	case isPercentKey(key):
		if number, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64); err == nil {
			field.Kind = ValuePercent
			field.Unit = "%"
			field.Number = float64Pointer(number)
		}
	case isByteKey(endpoint, key):
		if bytesValue, ok := parseByteCount(value); ok {
			field.Kind = ValueBytes
			field.Unit = "bytes"
			field.Integer = int64Pointer(bytesValue)
		}
	case key == "uptime" || key == "uptime_seconds":
		if key == "uptime_seconds" {
			if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
				field.Kind = ValueDuration
				field.Unit = "seconds"
				field.DurationSeconds = int64Pointer(seconds)
			}
		} else if seconds, ok := parseUptime(value); ok {
			field.Kind = ValueDuration
			field.Unit = "seconds"
			field.DurationSeconds = int64Pointer(seconds)
		}
	case isBooleanKey(key):
		if boolean, ok := parseBoolean(value); ok {
			field.Kind = ValueBoolean
			field.Boolean = boolPointer(boolean)
		}
	case isTimeKey(key):
		if parsed, ok := parseWireTime(value); ok {
			field.Kind = ValueTime
			field.Time = timePointer(parsed)
		}
	case isMACKey(key) && macPattern.MatchString(value):
		field.Kind = ValueMAC
	case isIPKey(key) && net.ParseIP(value) != nil:
		field.Kind = ValueIP
	case isHashKey(key):
		field.Kind = ValueHash
	case key == "state":
		field.Kind = ValueState
	default:
		if integer, err := strconv.ParseInt(value, 10, 64); err == nil &&
			isIntegerKey(key) {
			field.Kind = ValueInteger
			field.Integer = int64Pointer(integer)
		}
	}
	return field
}

func canonicalKey(input string) string {
	var output strings.Builder
	output.Grow(len(input))
	underscore := false
	var previous rune
	for _, character := range strings.TrimSpace(input) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			camelBoundary := unicode.IsUpper(character) &&
				(unicode.IsLower(previous) || unicode.IsDigit(previous))
			if (underscore || camelBoundary) && output.Len() > 0 {
				output.WriteByte('_')
			}
			output.WriteRune(unicode.ToLower(character))
			underscore = false
			previous = character
			continue
		}
		underscore = true
		previous = 0
	}
	return strings.Trim(output.String(), "_")
}

func humanizeKey(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	input = strings.NewReplacer("_", " ", ".", " · ", "-", " ").Replace(input)
	words := strings.Fields(input)
	for index, word := range words {
		upper := strings.ToUpper(word)
		switch upper {
		case "AP", "ESP", "ID", "IP", "MAC", "MD5", "RSSI", "SSID", "UDN", "URL", "WIFI":
			words[index] = upper
		default:
			if word != "" {
				words[index] = strings.ToUpper(word[:1]) + word[1:]
			}
		}
	}
	return strings.Join(words, " ")
}

func isSensitiveKey(key string) bool {
	compact := strings.ReplaceAll(key, "_", "")
	for _, marker := range []string{
		"password", "passwd", "passphrase", "secret", "token",
		"authorization", "credential", "cookie", "session", "wepkey",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	for _, short := range []string{"pass", "pwd", "psk"} {
		if key == short || strings.HasSuffix(key, "_"+short) {
			return true
		}
	}
	return false
}

func redactText(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	// Parsed JSON is redacted structurally. This defensive pass covers a
	// malformed/truncated JSON-looking response before it falls back to text.
	normalized = jsonSecretPattern.ReplaceAllString(
		normalized,
		`${1}"`+redactedValue+`"`,
	)
	normalized = jsonShortSecretPattern.ReplaceAllString(
		normalized,
		`${1}"`+redactedValue+`"`,
	)
	lines := strings.Split(normalized, "\n")
	for index, line := range lines {
		key, _, ok := splitKeyValueLine(strings.TrimSpace(line))
		if !ok || !isSensitiveKey(canonicalKey(key)) {
			continue
		}
		separator := strings.IndexAny(line, ":=")
		if separator >= 0 {
			lines[index] = line[:separator+1] + " " + redactedValue
		}
	}
	return strings.Join(lines, "\n")
}

func redactJSONValue(key string, value any) any {
	if isSensitiveKey(canonicalKey(key)) {
		return redactedValue
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, child := range typed {
			result[childKey] = redactJSONValue(childKey, child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactJSONValue(key, child)
		}
		return result
	default:
		return value
	}
}

func sanitizeSensitivePayload(input string) string {
	parseInput := strings.TrimSpace(strings.TrimRight(
		strings.TrimPrefix(input, "\ufeff"),
		"\x00",
	))
	if parseInput != "" && (parseInput[0] == '{' || parseInput[0] == '[') {
		if value, err := decodeSingleJSON([]byte(parseInput)); err == nil {
			if sanitized, marshalErr := json.MarshalIndent(
				redactJSONValue("", value),
				"",
				"  ",
			); marshalErr == nil {
				return string(sanitized)
			}
		}
	}
	return redactText(input)
}

func isSignalKey(key string) bool {
	return key == "rssi" || key == "signal" || key == "signal_strength" ||
		strings.HasSuffix(key, "_rssi")
}

func isPercentKey(key string) bool {
	return strings.Contains(key, "percent") || key == "program_usage" ||
		strings.HasSuffix(key, "_usage")
}

func isByteKey(endpoint, key string) bool {
	switch key {
	case "flash_size", "flash_real_size", "heap_memory", "free_heap",
		"heap_free", "size", "free", "firmware_size", "free_space":
		return endpoint == EndpointInfo || endpoint == EndpointUpdateInfo
	default:
		return strings.HasSuffix(key, "_bytes")
	}
}

func isBooleanKey(key string) bool {
	switch key {
	case "connected", "available", "valid", "corrupted", "protocol_match":
		return true
	default:
		return strings.HasPrefix(key, "is_") || strings.HasPrefix(key, "has_")
	}
}

func isTimeKey(key string) bool {
	switch key {
	case "time", "date", "build_time", "fetched_at":
		return true
	default:
		return strings.HasSuffix(key, "_time") || strings.HasSuffix(key, "_at")
	}
}

func isMACKey(key string) bool {
	return key == "mac" || strings.HasSuffix(key, "_mac")
}

func isIPKey(key string) bool {
	return key == "ip" || strings.HasSuffix(key, "_ip")
}

func isHashKey(key string) bool {
	return strings.Contains(key, "hash") || strings.HasSuffix(key, "_chip_id")
}

func isIntegerKey(key string) bool {
	for _, marker := range []string{"count", "clients", "attempt", "revision", "code"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func parseByteCount(value string) (int64, bool) {
	match := byteValuePattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	multiplier := float64(1)
	switch strings.ToUpper(match[2]) {
	case "KB", "KIB":
		multiplier = 1024
	case "MB", "MIB":
		multiplier = 1024 * 1024
	case "GB", "GIB":
		multiplier = 1024 * 1024 * 1024
	}
	result := number * multiplier
	if result > math.MaxInt64 {
		return 0, false
	}
	return int64(math.Round(result)), true
}

func parseUptime(value string) (int64, bool) {
	match := uptimePattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return 0, false
	}
	parse := func(input string) int64 {
		result, _ := strconv.ParseInt(input, 10, 64)
		return result
	}
	days := parse(match[1])
	hours := parse(match[2])
	minutes := parse(match[3])
	seconds := parse(match[4])
	if hours > 23 || minutes > 59 || seconds > 59 {
		return 0, false
	}
	return days*86400 + hours*3600 + minutes*60 + seconds, true
}

func parseBoolean(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled", "connected", "ok", "valid":
		return true, true
	case "0", "false", "no", "off", "disabled", "disconnected", "invalid":
		return false, true
	default:
		return false, false
	}
}

func parseWireTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006/01/02 15:04:05",
		"Jan _2 2006 15:04:05",
		"Jan 2 2006 15:04:05",
		"2006-01-02 15:04:05",
	} {
		if result, err := time.Parse(layout, value); err == nil {
			return result.UTC(), true
		}
	}
	return time.Time{}, false
}

func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }
func boolPointer(value bool) *bool          { return &value }
func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
