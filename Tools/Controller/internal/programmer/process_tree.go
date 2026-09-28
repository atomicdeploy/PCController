package programmer

import (
	"os"
)

type programmerProcessTree interface {
	Attach(*os.Process) error
	Terminate(*os.Process) error
	Close() error
}
