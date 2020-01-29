/*
Copyright 2018 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

func StringInSlice(haystack []string, needle string) bool {
	for i := range haystack {
		if haystack[i] == needle {
			return true
		}
	}
	return false
}

func StringsInSlice(haystack []string, needles ...string) bool {
	for _, needle := range needles {
		for i := range haystack {
			if haystack[i] == needle {
				return true
			}
		}
	}
	return false
}

func CompareStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

// FlattenStringSlice takes a slice of strings like ["one,two", "three"] and returns
// ["one", "two", "three"]
func FlattenStringSlice(slice []string) (retval []string) {
	for i := range slice {
		for _, role := range strings.Split(slice[i], ",") {
			retval = append(retval, strings.TrimSpace(role))
		}
	}
	return retval
}

// HasOneOfPrefixes returns true if the provided string starts with any of the specified prefixes
func HasOneOfPrefixes(s string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// MatchesLabels determines whether a set of "target" labels matches
// the set of "wanted" labels
func MatchesLabels(targetLabels, wantedLabels map[string]string) bool {
	for k, v := range wantedLabels {
		if targetLabels[k] != v {
			return false
		}
	}
	return true
}

// TrimPathPrefix returns the provided path without the specified prefix path
//
// Leading path separator is also stripped.
func TrimPathPrefix(path string, prefixPath ...string) string {
	return strings.TrimPrefix(path, filepath.Join(prefixPath...)+string(os.PathSeparator))
}

// CombineLabels combines the specified label sets into a single map.
// Existing labels will get overwritten with the last value
func CombineLabels(labels ...map[string]string) (result map[string]string) {
	result = make(map[string]string)
	for _, set := range labels {
		for k, v := range set {
			result[k] = v
		}
	}
	return result
}
