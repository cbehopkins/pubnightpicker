package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sweego_client/sweego"
)

func runTemplateUpload(args []string, client *sweego.Client, clientUUID string) error {
	fs := flag.NewFlagSet("template-upload", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var name string
	fs.StringVar(&name, "name", "", "template name (defaults to the file's base name)")

	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 1 {
		return errors.New("exactly one <template-file> argument is required")
	}

	path := positionals[0]
	source, err := readTemplateSource(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(path)
	}

	response, err := client.CreateTemplate(context.Background(), clientUUID, sweego.CreateTemplateRequest{
		Name:     name,
		Template: source,
	})
	if err != nil {
		return err
	}

	return reportTemplateResult(response)
}

func runTemplateUpdate(args []string, client *sweego.Client, clientUUID string) error {
	fs := flag.NewFlagSet("template-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var name string
	fs.StringVar(&name, "name", "", "template name (defaults to the file's base name)")

	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 2 {
		return errors.New("exactly two arguments are required: <template-uuid> <template-file>")
	}

	templateUUID := strings.TrimSpace(positionals[0])
	if templateUUID == "" {
		return errors.New("<template-uuid> cannot be blank")
	}

	path := positionals[1]
	source, err := readTemplateSource(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(path)
	}

	response, err := client.UpdateTemplate(context.Background(), clientUUID, templateUUID, sweego.UpdateTemplateRequest{
		Name:         name,
		Template:     source,
		TemplateType: "email",
	})
	if err != nil {
		return err
	}

	return reportTemplateResult(response)
}

func runTemplateDelete(args []string, client *sweego.Client, clientUUID string) error {
	fs := flag.NewFlagSet("template-delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 1 {
		return errors.New("exactly one <template-uuid> argument is required")
	}
	templateUUID := strings.TrimSpace(positionals[0])
	if templateUUID == "" {
		return errors.New("<template-uuid> cannot be blank")
	}

	response, err := client.DeleteTemplate(context.Background(), clientUUID, templateUUID)
	if err != nil {
		return err
	}

	printHTTPResult(response.Status, response.Body)
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	fmt.Printf("Deleted template: %s\n", templateUUID)
	return nil
}

// readTemplateSource returns the file verbatim: the template is whatever the
// caller wrote, never a locally converted or escaped derivative of it.
func readTemplateSource(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read template file: %w", err)
	}
	return string(content), nil
}

func reportTemplateResult(response sweego.HTTPResult) error {
	printHTTPResult(response.Status, response.Body)

	uuid, err := sweego.TemplateUUID(response.Body)
	if err != nil {
		fmt.Fprintln(osStderr, "template uuid parse warning:", err)
	} else {
		fmt.Printf("Template UUID: %s\n", uuid)
	}

	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	return nil
}
