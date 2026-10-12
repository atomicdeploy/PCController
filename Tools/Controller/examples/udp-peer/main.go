// udp-peer is an isolated, loopback-only developer probe. It never connects a
// board or starts host integrations, and never reads the user's configuration.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"pccontroller.local/controller/host"
	"pccontroller.local/controller/internal/appconfig"
	udp "pccontroller.local/controller/transport/udp"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--codec" {
		b, err := udp.Marshal(map[string]any{"signed": int64(-9223372036854775808), "unsigned": uint64(18446744073709551615), "float": 0.125, "unicode": "همتا", "array": []any{nil, false, true}})
		must(err)
		fmt.Println(hex.EncodeToString(b))
		return
	}
	root, err := os.MkdirTemp("", "pccontroller-udp-probe-")
	must(err)
	defer os.RemoveAll(root)
	path := filepath.Join(root, "config.json")
	store, err := appconfig.Open(path)
	must(err)
	_, err = store.Update(func(c *appconfig.Config) error {
		c.IPC.UDP = udp.Config{Enabled: true, Listen: "127.0.0.1:18877"}
		return nil
	})
	must(err)
	h, err := host.New(host.Options{ConfigPath: path, DataRoot: root, DisableAutoConnect: true, DisableNative: true, HTTP: &host.HTTPOptions{Address: "127.0.0.1:18878"}})
	must(err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	must(h.Start(ctx))
	defer h.Stop(context.Background())
	client, err := h.Controller()
	must(err)
	for i := 0; i < 30; i++ {
		var status map[string]any
		must(h.Call(ctx, "controller.udp.status", map[string]any{}, &status))
		if status["running"] == true {
			b, _ := json.Marshal(status)
			fmt.Println(string(b))
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			client.EmitHostEvent("udp_probe", "Isolated loopback event; no board connected")
		}
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
