package api

import (
	"fmt"
	"regexp"
)

var (
	projectExp = regexp.MustCompile(`^[a-zA-Z]+(?:-?[a-zA-Z0-9])*$`)
	serviceExp = regexp.MustCompile(`^[a-zA-Z]+[a-zA-Z0-9]*$`)
)

// VerifyNames checks that both project and service names are valid identifiers.
func VerifyNames(project, service string) error {
	if !projectExp.Match([]byte(project)) {
		return fmt.Errorf("invalid project name: may only contains alphanumerics and dashes")
	}
	if !serviceExp.Match([]byte(service)) {
		return fmt.Errorf("invalid service name: may only contain alphanumerics")
	}
	return nil
}
