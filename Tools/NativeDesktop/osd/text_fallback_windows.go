//go:build windows && !amd64

package osd

func newDirectWriteRasterizer() (textRasterizer, error) { return nil, ErrUnsupported }
