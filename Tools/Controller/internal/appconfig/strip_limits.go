package appconfig

// StripMaximumFPS is the configurable host-stream ceiling, not a promise that
// every transport/pixel count can deliver this rate. Streams remain ACK-paced.
const StripMaximumFPS = 120
