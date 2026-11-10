//go:build no_mqtt

package mqttbridge

import "context"

const Available = false

func Run(context.Context, Config, Hooks) {}
