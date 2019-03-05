package main

var version string

func getVersion() string {
	if version == "" {
		return "unknown"
	}

	return version
}
