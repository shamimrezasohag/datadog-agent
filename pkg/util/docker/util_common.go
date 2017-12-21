// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2017 Datadog, Inc.

package docker

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DataDog/datadog-agent/pkg/config"
)

// ErrNotImplemented is the "not implemented" error given by `gopsutil` when an
// OS doesn't support and API. Unfortunately it's in an internal package so
// we can't import it so we'll copy it here.
var ErrNotImplemented = errors.New("not implemented yet")

// readLines reads contents from a file and splits them by new lines.
func readLines(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return []string{""}, err
	}
	defer f.Close()

	var ret []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ret = append(ret, scanner.Text())
	}
	return ret, scanner.Err()
}

// getEnv retrieves the environment variable key. If it does not exist it returns the default.
func getEnv(key string, dfault string, combineWith ...string) string {
	value := os.Getenv(key)
	if value == "" {
		value = dfault
	}

	switch len(combineWith) {
	case 0:
		return value
	case 1:
		return filepath.Join(value, combineWith[0])
	default:
		var b bytes.Buffer
		b.WriteString(value)
		for _, v := range combineWith {
			b.WriteRune('/')
			b.WriteString(v)
		}
		return b.String()
	}
}

// hostProc returns the location of a host's procfs. This can and will be
// overridden when running inside a container.
func hostProc(combineWith ...string) string {
	parts := append([]string{config.Datadog.GetString("container_proc_root")}, combineWith...)
	return path.Join(parts...)
}

// pathExists returns a boolean indicating if the given path exists on the file system.
func pathExists(filename string) bool {
	if _, err := os.Stat(filename); err == nil {
		return true
	}
	return false
}

// SplitImageName splits a valid image name (from ResolveImageName) and returns:
//    - the "long image name" with registry and prefix, without tag
//    - the "short image name", without registry, prefix nor tag
//    - the image tag if present
//    - an error if parsing failed
func SplitImageName(image string) (string, string, string, error) {
	// See TestSplitImageName for supported formats (number 6 will surprise you!)
	if image == "" {
		return "", "", "", errors.New("empty image name")
	}
	long := image
	if pos := strings.LastIndex(long, "@sha"); pos > 0 {
		// Remove @sha suffix when orchestrator is sha-pinning
		long = long[0:pos]
	}

	var short, tag string
	lastColon := strings.LastIndex(long, ":")
	lastSlash := strings.LastIndex(long, "/")

	if lastColon > -1 && lastColon > lastSlash {
		// We have a tag
		tag = long[lastColon+1:]
		long = long[:lastColon]
	}
	if lastSlash > -1 {
		// we have a prefix / registry
		short = long[lastSlash+1:]
	} else {
		short = long
	}
	return long, short, tag, nil
}
