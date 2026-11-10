package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"time"

	"pccontroller.local/controller/internal/boardsensors"
)

func runSensors(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "hwmon" {
		return errors.New("usage: sensors hwmon --url http://127.0.0.1:8787")
	}
	flags := flag.NewFlagSet("sensors hwmon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("url", "http://127.0.0.1:8787", "Existing local PCController HTTP listener")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected sensor bridge arguments")
	}
	u, err := url.Parse(*endpoint)
	if err != nil {
		return err
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("sensor bridge requires a literal loopback HTTP origin")
	}
	if runtime.GOOS != "linux" {
		return errors.New("hwmon is Linux-only; Windows consumers use /api/sensors/metrics")
	}
	const device = "/dev/pccontroller-hwmon"
	info, err := os.Lstat(device)
	if err != nil {
		return fmt.Errorf("load pccontroller_hwmon first: %w", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return errors.New("sensor bridge target is not the kernel character device")
	}
	fd, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer fd.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u.Path = "/api/sensors/board"
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}
	defer client.CloseIdleConnections()
	fmt.Fprintln(stdout, "PCController hwmon bridge active")
	lastError := ""
	for {
		frame, readErr := readBoardSensorFrame(ctx, client, u.String())
		if readErr != nil {
			frame = "1 0 0 0 0 0 0 0 0\n"
		}
		if _, err = io.WriteString(fd, frame); err != nil {
			return err
		}
		message := ""
		if readErr != nil {
			message = readErr.Error()
		}
		if message != lastError {
			if message != "" {
				fmt.Fprintln(stderr, message)
			}
			lastError = message
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}

func readBoardSensorFrame(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if token := os.Getenv("PCCONTROLLER_SENSOR_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sensor API: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return "", err
	}
	if len(data) > 65536 {
		return "", errors.New("sensor API response exceeds 64 KiB")
	}
	var snapshot boardsensors.Snapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return "", err
	}
	return snapshot.HWMonFrame(time.Now())
}
