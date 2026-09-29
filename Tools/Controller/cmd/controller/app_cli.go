package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
)

type surfaceLaunchCLIOptions struct {
	Request        hostui.SurfaceLaunchRequest
	Address        string
	Peer           string
	TokenReference string
	Timeout        time.Duration
}

func runApp(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	if len(args) == 0 || !strings.EqualFold(args[0], "launch") {
		return errors.New("usage: controller app launch tui|webui [--mode ensure|launch|focus] [--target INSTANCE] [--page PAGE] [--peer NAME]")
	}
	options, err := parseSurfaceLaunchCLI(args[1:], stderr, store.CurrentRuntime().IPC.Listen)
	if err != nil {
		return err
	}
	request, err := hostui.NormalizeSurfaceLaunchRequest(options.Request)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	var result hostui.SurfaceLaunchResult
	if options.Peer != "" {
		var bridged struct {
			Peer     string           `json:"peer"`
			Response ipcjson.Response `json:"response"`
		}
		err = callPrimary(ctx, "controller.bridge.call", map[string]any{
			"peer": options.Peer,
			"request": ipcjson.Request{
				JSONRPC: ipcjson.Version, ID: json.RawMessage("1"),
				Method: "controller.app.launch", Params: encoded,
			},
		}, &bridged)
		if err == nil && bridged.Response.Error != nil {
			err = fmt.Errorf("peer %s: %s", options.Peer, bridged.Response.Error.Message)
		}
		if err == nil {
			var response []byte
			response, err = json.Marshal(bridged.Response.Result)
			if err == nil {
				err = json.Unmarshal(response, &result)
			}
		}
	} else {
		auth := ""
		configured := currentPrimaryEndpoint()
		if strings.EqualFold(strings.TrimSpace(options.Address), strings.TrimSpace(configured.Listen)) {
			auth = configured.AuthToken
		}
		if options.TokenReference != "" {
			auth, err = store.ResolveSecret(options.TokenReference)
			if err != nil {
				return fmt.Errorf("resolve IPC bearer token: %w", err)
			}
		}
		err = callPrimaryAtAuthenticated(
			ctx, options.Address, auth, "controller.app.launch", request, &result,
		)
	}
	if err != nil {
		return err
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(output))
	return nil
}

func parseSurfaceLaunchCLI(
	args []string,
	stderr io.Writer,
	defaultAddress string,
) (surfaceLaunchCLIOptions, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return surfaceLaunchCLIOptions{}, errors.New("app launch requires tui or webui")
	}
	options := surfaceLaunchCLIOptions{
		Request: hostui.SurfaceLaunchRequest{Surface: args[0]},
		Address: defaultAddress, Timeout: 15 * time.Second,
	}
	flags := flag.NewFlagSet("app launch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.Request.Mode, "mode", "ensure", "ensure, launch, or focus the named surface")
	flags.StringVar(&options.Request.Target, "target", "", "exact live instance ID for ensure/focus")
	flags.StringVar(&options.Request.Page, "page", "", "bounded application page to open")
	flags.StringVar(&options.Request.IdempotencyKey, "idempotency-key", "", "deduplicate retried launch requests")
	flags.StringVar(&options.Address, "addr", defaultAddress, "coordinator IPC address")
	flags.StringVar(&options.Peer, "peer", "", "launch through one configured bridge peer")
	flags.StringVar(&options.TokenReference, "token-ref", "", "resolve a direct remote IPC token from the OS vault/environment")
	flags.DurationVar(&options.Timeout, "timeout", 15*time.Second, "request timeout")
	if err := flags.Parse(args[1:]); err != nil {
		return surfaceLaunchCLIOptions{}, err
	}
	if flags.NArg() != 0 {
		return surfaceLaunchCLIOptions{}, errors.New("app launch does not accept executable names, arguments, or shell text")
	}
	addressWasSet := false
	flags.Visit(func(value *flag.Flag) {
		addressWasSet = addressWasSet || value.Name == "addr"
	})
	options.Peer = strings.TrimSpace(options.Peer)
	options.TokenReference = strings.TrimSpace(options.TokenReference)
	if options.Peer != "" && (addressWasSet || options.TokenReference != "") {
		return surfaceLaunchCLIOptions{}, errors.New("--peer cannot be combined with --addr or --token-ref")
	}
	if options.Timeout <= 0 || options.Timeout > time.Minute {
		return surfaceLaunchCLIOptions{}, errors.New("app launch timeout must be greater than zero and at most 1m")
	}
	return options, nil
}
