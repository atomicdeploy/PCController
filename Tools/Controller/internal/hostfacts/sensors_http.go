package hostfacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

const defaultLibreSensorsURL = "http://127.0.0.1:8085/data.json"

var sensorHTTPClient = &http.Client{
	Timeout:       DefaultQueryTimeout,
	Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func validateSensorsURL(value string) (string, error) {
	if value == "" {
		value = defaultLibreSensorsURL
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", errors.New("invalid local sensor endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/data.json" || u.RawPath != "" || u.Opaque != "" {
		return "", errors.New("sensor endpoint must be a literal loopback HTTP address ending in /data.json, without credentials or query")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("invalid local sensor port")
		}
	}
	return u.String(), nil
}

func windowsSensorFacts(ctx context.Context) ([]map[string]any, bool, error) {
	endpoint, err := validateSensorsURL(os.Getenv("PCCONTROLLER_HOST_SENSORS_URL"))
	if err != nil {
		return nil, false, err
	}
	return readLibreSensors(ctx, sensorHTTPClient, endpoint)
}

func readLibreSensors(ctx context.Context, client *http.Client, endpoint string) ([]map[string]any, bool, error) {
	// Revalidate even internal callers; no caller-selected remote host, mutation
	// route, redirect, system proxy or ambient authentication can cross this path.
	endpoint, err := validateSensorsURL(endpoint)
	if err != nil {
		return nil, false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, false, errors.New("PC sensors unavailable: start Libre Hardware Monitor and enable its local web server")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("Libre Hardware Monitor returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSensorInputBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read PC sensor response: %w", err)
	}
	root, err := decodeLibreTree(data)
	if err != nil {
		return nil, false, err
	}
	// Some publishers expose formatted RawValue strings even though their Get
	// operation returns proper nullable numbers. Read that advertised operation,
	// never guess locale/units from the display string. One shared cached query
	// owns this bounded batch, not a poller for each client or rendered frame.
	if err := resolveLibreNumbers(ctx, client, endpoint, &root); err != nil {
		return nil, false, err
	}
	return libreSensorRows(root)
}

func resolveLibreNumbers(ctx context.Context, client *http.Client, endpoint string, root *lhmNode) error {
	var nodes []*lhmNode
	var visit func(*lhmNode, int) error
	visit = func(node *lhmNode, depth int) error {
		if depth > 32 {
			return errors.New("sensor tree exceeds 32 levels")
		}
		if node.SensorID != "" {
			nodes = append(nodes, node)
		}
		for index := range node.Children {
			if err := visit(&node.Children[index], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return err
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].SensorID < nodes[j].SensorID })
	if len(nodes) > maxGlobalRows {
		nodes = nodes[:maxGlobalRows]
	}
	jobs := make(chan *lhmNode, len(nodes))
	for _, node := range nodes {
		if _, formatted := node.Value.(string); formatted {
			jobs <- node
		}
	}
	close(jobs)
	if len(jobs) == 0 {
		return ctx.Err()
	}
	queryContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	var firstError error
	var once sync.Once
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for node := range jobs {
				if queryContext.Err() != nil {
					return
				}
				if err := readLibreNumber(queryContext, client, endpoint, node); err != nil {
					once.Do(func() { firstError = err; cancel() })
					return
				}
			}
		}()
	}
	group.Wait()
	if firstError != nil {
		return firstError
	}
	return ctx.Err()
}

func readLibreNumber(ctx context.Context, client *http.Client, endpoint string, node *lhmNode) error {
	u, _ := url.Parse(endpoint) // already validated immutable local origin
	u.Path = "/Sensor"
	u.RawQuery = url.Values{"action": {"Get"}, "id": {node.SensorID}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("Libre Hardware Monitor numeric read failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Libre Hardware Monitor numeric read returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 16*1024 {
		return errors.New("Libre Hardware Monitor numeric read exceeds 16 KiB")
	}
	var result struct {
		Result string          `json:"result"`
		Value  json.RawMessage `json:"value"`
		Min    any             `json:"min"`
		Max    any             `json:"max"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Result != "ok" || len(result.Value) == 0 {
		return errors.New("invalid Libre Hardware Monitor numeric response")
	}
	var value any
	if err := json.Unmarshal(result.Value, &value); err != nil {
		return err
	}
	if value != nil {
		if _, ok := sensorNumber(value); !ok {
			if named, ok := value.(string); ok && (named == "NaN" || named == "Infinity" || named == "-Infinity") {
				value = nil
			} else {
				return errors.New("Libre Hardware Monitor returned a nonnumeric reading")
			}
		}
	}
	node.Value, node.Min, node.Max = value, result.Min, result.Max
	return nil
}
