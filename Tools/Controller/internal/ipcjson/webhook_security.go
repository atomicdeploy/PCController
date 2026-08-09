package ipcjson

import (
	"sort"
	"strings"
)

var inboundWebhookHeaderAllowlist = map[string]bool{
	"content-type":     true,
	"traceparent":      true,
	"tracestate":       true,
	"user-agent":       true,
	"x-correlation-id": true,
	"x-request-id":     true,
}

var inboundWebhookReservedMetadata = map[string]bool{
	"authentication": true,
	"claimed_source": true,
	"correlation_id": true,
	"origin":         true,
	"principal":      true,
	"transport":      true,
}

var inboundWebhookSensitiveTerms = []string{
	"apikey",
	"authorization",
	"cookie",
	"credential",
	"password",
	"passwd",
	"referrer",
	"referer",
	"secret",
	"session",
	"signature",
	"ticket",
	"token",
}

func inboundWebhookSensitiveName(value string) bool {
	compact := strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') {
			return character
		}
		return -1
	}, value)
	for _, term := range inboundWebhookSensitiveTerms {
		if strings.Contains(compact, term) {
			return true
		}
	}
	return false
}

// sanitizeInboundWebhookMetadata treats caller-supplied metadata as data, not
// transport provenance. Reserved namespaces and secret-shaped names are
// removed before the host adds its own bounded provenance fields.
func sanitizeInboundWebhookMetadata(values map[string]string, maximum int) map[string]string {
	result := make(map[string]string)
	if maximum <= 0 {
		return result
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := strings.TrimSpace(key)
		lower := strings.ToLower(name)
		if name == "" || len(name) > 64 || inboundWebhookReservedMetadata[lower] ||
			inboundWebhookSensitiveName(name) || strings.HasPrefix(lower, "http.") ||
			strings.HasPrefix(lower, "query.") || strings.HasPrefix(lower, "header.") ||
			strings.HasPrefix(lower, "security.") {
			continue
		}
		result[name] = truncateText(values[key], 1024)
		if len(result) >= maximum {
			break
		}
	}
	return result
}

func appendInboundWebhookQueryMetadata(
	metadata map[string]string,
	query map[string][]string,
	maximum int,
) {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(metadata) >= maximum || inboundWebhookSensitiveName(key) {
			continue
		}
		metadata["query."+strings.ToLower(key)] =
			truncateText(strings.Join(query[key], ","), 1024)
	}
}

func appendInboundWebhookHeaderMetadata(
	metadata map[string]string,
	headers map[string][]string,
	maximum int,
) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := strings.ToLower(strings.TrimSpace(key))
		if len(metadata) >= maximum || !inboundWebhookHeaderAllowlist[name] ||
			inboundWebhookSensitiveName(name) {
			continue
		}
		metadata["header."+name] = truncateText(strings.Join(headers[key], ","), 1024)
	}
}
