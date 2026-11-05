package buildinfo

const (
	Name        = "PCController"
	DisplayName = "PCController"
	Version     = "1.0.0"
	Company     = "AtomicDeploy"
	Description = "PCController native desktop interface"
	Copyright   = "Copyright © 2026"
	PipeName    = `\\.\pipe\PCControllerDesktop`

	EditionDevelop    = "develop"
	EditionProduction = "production"
)

var (
	Commit = "dev"
	Date   = "unknown"
)
