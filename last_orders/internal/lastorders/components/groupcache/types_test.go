package groupcache

import "testing"

func TestGroupFromDocumentValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		want    Group
		wantErr bool
	}{
		{name: "public", data: map[string]any{"name": "North", "visibility": "public"}, want: Group{ID: "opaque-id", Name: "North", Visibility: VisibilityPublic}},
		{name: "private", data: map[string]any{"name": "South", "visibility": "private"}, want: Group{ID: "opaque-id", Name: "South", Visibility: VisibilityPrivate}},
		{name: "missing name", data: map[string]any{"visibility": "public"}, wantErr: true},
		{name: "empty name", data: map[string]any{"name": "", "visibility": "public"}, wantErr: true},
		{name: "non-string name", data: map[string]any{"name": 3, "visibility": "public"}, wantErr: true},
		{name: "missing visibility", data: map[string]any{"name": "North"}, wantErr: true},
		{name: "unsupported visibility", data: map[string]any{"name": "North", "visibility": "members"}, wantErr: true},
		{name: "case-sensitive visibility", data: map[string]any{"name": "North", "visibility": "Public"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := GroupFromDocument(Document{ID: "opaque-id", Data: test.data})
			if (err != nil) != test.wantErr {
				t.Fatalf("GroupFromDocument() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("GroupFromDocument() = %+v, want %+v", got, test.want)
			}
		})
	}
}
