//go:build !windows

package ownedstorage

import "os/user"

func CurrentOwnerID() (string, error) {
	value, err := user.Current()
	if err != nil {
		return "", err
	}
	return "uid:" + value.Uid, nil
}
