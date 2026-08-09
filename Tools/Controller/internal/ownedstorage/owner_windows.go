//go:build windows

package ownedstorage

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

func CurrentOwnerID() (string, error) {
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("resolve current Windows user SID: %w", err)
	}
	if owner == nil || owner.User.Sid == nil {
		return "", errors.New("current Windows user SID is unavailable")
	}
	return owner.User.Sid.String(), nil
}
