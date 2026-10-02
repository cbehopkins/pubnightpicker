package texttemplate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func Validate(source string) error {
	remaining := placeholder.ReplaceAllString(source, "")
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return errors.New("placeholders must use {{name}} or {{ name }} syntax")
	}
	return nil
}

func Render(source string, variables map[string]any) (string, error) {
	if err := Validate(source); err != nil {
		return "", err
	}
	var missing error
	rendered := placeholder.ReplaceAllStringFunc(source, func(match string) string {
		name := placeholder.FindStringSubmatch(match)[1]
		value, ok := variables[name]
		if !ok {
			if missing == nil {
				missing = fmt.Errorf("missing variable %q", name)
			}
			return match
		}
		return fmt.Sprint(value)
	})
	return rendered, missing
}
