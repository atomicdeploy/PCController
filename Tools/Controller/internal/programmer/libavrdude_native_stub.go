//go:build !cgo || !libavrdude

package programmer

import (
	"context"
	"errors"
	"io"
)

const nativeAvrdudeAvailable = false

func nativeAvrdudeModulePath() string { return "" }

func runNativeAvrdude(context.Context, Command, io.Writer) error {
	return errors.New("this build has no embedded libavrdude; rebuild with CGO and the libavrdude tag or select the helper/cli backend")
}
