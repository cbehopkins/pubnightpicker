package groupcache

import (
	"context"
	"fmt"
)

type GroupID string

type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

type Group struct {
	ID         GroupID
	Name       string
	Visibility Visibility
}

type Document struct {
	ID   string
	Data map[string]any
}

type ChangeKind int

const (
	ChangeAdded ChangeKind = iota
	ChangeModified
	ChangeRemoved
)

type Change struct {
	Kind ChangeKind
	Doc  Document
}

type ChangeStream interface {
	Next() ([]Change, error)
	Stop()
}

type Source interface {
	Watch(context.Context) (ChangeStream, error)
}

func GroupFromDocument(doc Document) (Group, error) {
	if doc.ID == "" {
		return Group{}, fmt.Errorf("group document ID is required")
	}
	name, ok := doc.Data["name"].(string)
	if !ok || name == "" {
		return Group{}, fmt.Errorf("group %q field %q must be a non-empty string", doc.ID, "name")
	}
	visibilityValue, ok := doc.Data["visibility"].(string)
	if !ok {
		return Group{}, fmt.Errorf("group %q field %q must be a string", doc.ID, "visibility")
	}
	visibility := Visibility(visibilityValue)
	if visibility != VisibilityPublic && visibility != VisibilityPrivate {
		return Group{}, fmt.Errorf("group %q field %q has unsupported value %q", doc.ID, "visibility", visibilityValue)
	}
	return Group{ID: GroupID(doc.ID), Name: name, Visibility: visibility}, nil
}
