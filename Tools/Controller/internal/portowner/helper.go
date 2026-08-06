package portowner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	ownerHelperArgument   = "--internal-port-owner-scan"
	ownerDiagnoseArgument = "--internal-port-owner-diagnose"
	ownerHelperVersion    = 1
	maxOwnerHelperOutput  = 16 * 1024
	maxOwnerHelperError   = 2 * 1024
)

type ownerHelperResult struct {
	Version int    `json:"version"`
	Port    string `json:"port"`
	Found   bool   `json:"found"`
	Owner   *Owner `json:"owner,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ownerLookupFunc func(context.Context, string) (Owner, bool, error)

// IsHelperInvocation identifies the private, read-only serial-owner helper.
func IsHelperInvocation(args []string) bool {
	return len(args) > 0 && (args[0] == ownerHelperArgument || args[0] == ownerDiagnoseArgument)
}

// RunHelperInvocation performs exactly one bounded scan and writes one JSON line.
func RunHelperInvocation(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == ownerDiagnoseArgument {
		return runHelperInvocationWith(ctx, args, stdout, systemEnumerator().FindOwner)
	}
	return runHelperInvocationWith(ctx, args, stdout, scanLegacyNativeOwner)
}

func runHelperInvocationWith(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	scan ownerLookupFunc,
) error {
	if len(args) != 2 || !IsHelperInvocation(args) {
		return errors.New("invalid internal serial-owner helper invocation")
	}
	port := normalizeHelperPort(args[1])
	if !validHelperPort(port) {
		return errors.New("internal serial-owner helper requires an exact COM number")
	}
	if stdout == nil || scan == nil {
		return errors.New("internal serial-owner helper is not initialized")
	}
	owner, found, scanErr := scan(ctx, port)
	result := ownerHelperResult{Version: ownerHelperVersion, Port: port, Found: found}
	if found && scanErr == nil {
		owner = boundedOwner(owner)
		result.Owner = &owner
	}
	if scanErr != nil {
		result.Found = false
		result.Error = truncateUTF8(sanitizeOwnerText(scanErr.Error()), maxOwnerHelperError)
	}
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(result); err != nil {
		return fmt.Errorf("encode serial-owner helper result: %w", err)
	}
	if encoded.Len() > maxOwnerHelperOutput {
		return fmt.Errorf("serial-owner helper result exceeded %d bytes", maxOwnerHelperOutput)
	}
	_, err := stdout.Write(encoded.Bytes())
	return err
}

func decodeOwnerHelperResult(port string, encoded []byte) (Owner, bool, error) {
	if len(encoded) == 0 {
		return Owner{}, false, errors.New("serial-owner helper returned no JSON")
	}
	if len(encoded) > maxOwnerHelperOutput {
		return Owner{}, false, fmt.Errorf("serial-owner helper output exceeded %d bytes", maxOwnerHelperOutput)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var result ownerHelperResult
	if err := decoder.Decode(&result); err != nil {
		return Owner{}, false, fmt.Errorf("decode serial-owner helper JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Owner{}, false, errors.New("serial-owner helper returned more than one JSON value")
		}
		return Owner{}, false, fmt.Errorf("decode serial-owner helper trailer: %w", err)
	}
	port = normalizeHelperPort(port)
	if result.Version != ownerHelperVersion || result.Port != port {
		return Owner{}, false, errors.New("serial-owner helper identity mismatch")
	}
	if result.Error != "" {
		return Owner{}, false, errors.New(truncateUTF8(sanitizeOwnerText(result.Error), maxOwnerHelperError))
	}
	if result.Found != (result.Owner != nil) {
		return Owner{}, false, errors.New("serial-owner helper returned an inconsistent owner result")
	}
	if !result.Found {
		return Owner{}, false, nil
	}
	if result.Owner.PID == 0 {
		return Owner{}, false, errors.New("serial-owner helper returned an owner without a PID")
	}
	return boundedOwner(*result.Owner), true, nil
}

func normalizeHelperPort(port string) string {
	port = strings.ToUpper(strings.TrimSpace(port))
	port = strings.TrimPrefix(port, `\\.\`)
	return strings.TrimPrefix(port, `\\?\`)
}

func validHelperPort(port string) bool {
	return len(port) <= 15 && isCOMPort(port)
}

func boundedOwner(owner Owner) Owner {
	owner.Name = truncateUTF8(sanitizeOwnerText(owner.Name), 512)
	owner.Executable = truncateUTF8(sanitizeOwnerText(owner.Executable), 4096)
	owner.Window.Title = truncateUTF8(sanitizeOwnerText(owner.Window.Title), 1024)
	owner.Window.Class = truncateUTF8(sanitizeOwnerText(owner.Window.Class), 256)
	return owner
}

func sanitizeOwnerText(value string) string {
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
