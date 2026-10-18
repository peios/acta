package migration

import "testing"

func TestFieldCatalogueCannotChangeValidation(t *testing.T) {
	catalogue := Fields()
	catalogue["task"][0] = "credentials"
	delete(catalogue, "workspace")
	allowed := FieldsFor("task")
	if allowed[0] != "workspace_id" || FieldsFor("workspace") == nil {
		t.Fatal("catalogue caller changed migration policy")
	}
	allowed[0] = "credentials"
	if FieldsFor("task")[0] != "workspace_id" {
		t.Fatal("field caller changed migration policy")
	}
	if FieldsFor("unknown") != nil {
		t.Fatal("unknown kind has an allowlist")
	}
}
