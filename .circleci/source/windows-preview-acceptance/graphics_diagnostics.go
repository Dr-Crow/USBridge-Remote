package main

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

var graphicsDLLName = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,100}\.[dD][lL][lL]$`)

// A rejection stays fatal. These bounded hints describe only the first rejected
// module of the already-owned process; no full path or unrelated PID is emitted.
type graphicsModuleRejection struct {
	Name     string `json:"module_basename,omitempty"`
	Location string `json:"location_category"`
	Declared bool   `json:"declared_in_staging"`
}

type graphicsModuleFailure struct {
	code      string
	rejection graphicsModuleRejection
}

func (e *graphicsModuleFailure) Error() string { return e.code }

func graphicsRejection(err error) *graphicsModuleRejection {
	var failure *graphicsModuleFailure
	if !errors.As(err, &failure) {
		return nil
	}
	copy := failure.rejection
	return &copy
}

func rejectedGraphicsModule(code, name, modulePath, directory, systemRoot string, declared bool) error {
	normalize := func(s string) string {
		return strings.ToLower(path.Clean(strings.ReplaceAll(strings.TrimPrefix(s, `\\?\`), `\`, `/`)))
	}
	module, app, windows := normalize(modulePath), normalize(directory), normalize(systemRoot)
	in := func(root string) bool { return strings.HasPrefix(module, strings.TrimRight(root, "/")+"/") }
	location := "other"
	switch {
	case path.Dir(module) == app:
		location = "app_local"
	case in(windows + "/system32"):
		location = "windows_system32"
	case in(windows + "/winsxs"):
		location = "windows_side_by_side"
	case in(windows):
		location = "windows_other"
	}
	r := graphicsModuleRejection{Location: location, Declared: declared}
	if graphicsDLLName.MatchString(name) {
		r.Name = name
	}
	return &graphicsModuleFailure{code: code, rejection: r}
}
