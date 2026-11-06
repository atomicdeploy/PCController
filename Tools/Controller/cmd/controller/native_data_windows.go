//go:build windows && !no_native_gui

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pccontroller.local/desktop/desktop"
)

const nativeDataLimit = 8 << 20

func (p nativePages) nativeOwnerHTTP(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if p.rest != nil {
		return p.rest(ctx, method, path, body)
	}
	if p.client == nil {
		return nil, errors.New("controller connection unavailable")
	}
	if !strings.HasPrefix(path, "/api/integrations/datahub/") {
		return nil, errors.New("request is outside the DataHub owner route")
	}
	endpoint := url.URL{Scheme: "http", Host: p.client.address}
	relative, err := url.ParseRequestURI(path)
	if err != nil || relative.IsAbs() || relative.Host != "" {
		return nil, errors.New("invalid owner-relative request path")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = relative.Path, relative.RawPath, relative.RawQuery
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.client.auth != "" {
		request.Header.Set("X-PCController-Token", p.client.auth)
	}
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeDataLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > nativeDataLimit {
		return nil, errors.New("DataHub response exceeds the 8 MiB native workbench limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("DataHub HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (p nativePages) loadNativeDataPage() desktop.Page {
	return desktop.Page{Title: "Data workspace", Fields: []desktop.Field{
		field("path", "Relative data path", "v1/status"), choice("method", "HTTP method", "GET", "GET", "POST"), {Key: "body", Label: "JSON body", Value: "{\n  \"query\": \"\"\n}", Multiline: true},
		{Key: "export_path", Label: "Export response to new file", FileFilter: "JSON (*.json)|*.json|All files (*.*)|*.*", SaveFile: true, Section: "Export"},
	}, Actions: []desktop.PageAction{{ID: "request", Label: "Send request", Enabled: !p.preview, TimeoutSeconds: 35}, {ID: "export", Label: "Download to file", Enabled: !p.preview, TimeoutSeconds: 35}}}
}

func nativeDataRoute(path string) (string, error) {
	path = strings.TrimSpace(path)
	parsed, err := url.Parse(path)
	if err != nil || path == "" || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" || strings.HasPrefix(path, "//") || strings.Contains(parsed.Path, "\\") {
		return "", errors.New("enter a relative DataHub path, not an origin or URL")
	}
	for _, part := range strings.Split(parsed.Path, "/") {
		if part == ".." || part == "." {
			return "", errors.New("relative path traversal is not allowed")
		}
	}
	return "/api/integrations/datahub/" + strings.TrimLeft(path, "/"), nil
}

func (p nativePages) submitNativeData(ctx context.Context, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot query integrations")
	}
	if action != "request" && action != "export" {
		return desktop.Page{}, errors.New("unknown data operation")
	}
	path, err := nativeDataRoute(values["path"])
	if err != nil {
		return desktop.Page{}, err
	}
	method := values["method"]
	if action == "export" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		return desktop.Page{}, errors.New("only GET and POST are supported")
	}
	var body []byte
	if method == "POST" {
		body = []byte(values["body"])
		if len(body) > 1<<20 || !json.Valid(body) {
			return desktop.Page{}, errors.New("body must be valid JSON no larger than 1 MiB")
		}
	}
	data, err := p.nativeOwnerHTTP(ctx, method, path, body)
	if err != nil {
		return desktop.Page{}, err
	}
	page := p.loadNativeDataPage()
	for index := range page.Fields {
		if value, ok := values[page.Fields[index].Key]; ok {
			page.Fields[index].Value = value
		}
	}
	if action == "export" {
		path := values["export_path"]
		if !filepath.IsAbs(path) {
			return desktop.Page{}, errors.New("choose an absolute export filename")
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return desktop.Page{}, err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return desktop.Page{}, fmt.Errorf("export incomplete at %s: %w", path, err)
		}
		page.Output = fmt.Sprintf("Saved %d bytes to %s", len(data), path)
		return page, nil
	}
	page.Output = string(data)
	var payload any
	if json.Unmarshal(data, &payload) == nil {
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		page.Output = string(encoded)
		var records []any
		switch value := payload.(type) {
		case []any:
			records = value
		case map[string]any:
			if rows, ok := value["records"].([]any); ok {
				records = rows
			} else if rows, ok := value["data"].([]any); ok {
				records = rows
			} else {
				records = []any{value}
			}
		}
		columns := map[string]bool{}
		for _, record := range records {
			if row, ok := record.(map[string]any); ok {
				for key := range row {
					columns[key] = true
				}
			}
		}
		for key := range columns {
			page.Columns = append(page.Columns, key)
		}
		sort.Strings(page.Columns)
		if len(page.Columns) > 32 {
			page.Columns = page.Columns[:32]
		}
		for _, record := range records[:min(len(records), 1000)] {
			row, ok := record.(map[string]any)
			if !ok {
				continue
			}
			var cells []string
			for _, column := range page.Columns {
				if v, exists := row[column]; exists {
					if text, ok := v.(string); ok {
						cells = append(cells, text)
					} else {
						encoded, _ := json.Marshal(v)
						cells = append(cells, string(encoded))
					}
				} else {
					cells = append(cells, "")
				}
			}
			page.Rows = append(page.Rows, cells)
		}
	}
	return page, nil
}
