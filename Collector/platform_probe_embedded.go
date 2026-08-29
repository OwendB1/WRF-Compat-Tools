//go:build platformprobe

package main

import _ "embed"

//go:embed dist/wrf-platform-probe.exe
var platformProbeBinary []byte
