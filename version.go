package main

import (
	_ "embed"
	"encoding/json"
	"runtime/debug"
)

var version string

//go:embed version.json
var versionJSON []byte

func init() {
	// Read version from embedded JSON file.
	var verMap map[string]string
	json.Unmarshal(versionJSON, &verMap)
	version = verMap["version"]

	// If running from a module, try to get the build info.
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	// If rcs is modified, then append the revision to the version.
	var modified bool
	var revision string
	var found int
	for i := range bi.Settings {
		switch bi.Settings[i].Key {
		case "vcs.modified":
			found++
			modified = bi.Settings[i].Value == "true"
		case "vcs.revision":
			found++
			revision = bi.Settings[i].Value
		}
		if found == 2 {
			break
		}
	}
	if modified {
		version += "+" + revision
	}
}
