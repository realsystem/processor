package jobtypes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func obj(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func frames(t *testing.T) []Type {
	return []Type{
		{Name: "video.frames", Version: "1.2", InputSchema: obj(t, `{
			"type":"object","required":["source","fps"],"additionalProperties":false,
			"properties":{"source":{"type":"string","pattern":"^https://"},"fps":{"type":"integer","minimum":1,"maximum":30},
			"tags":{"type":"array","items":{"type":"string"},"maxItems":2}}}`),
			ResultSchema: obj(t, `{"type":"object","required":["frames"],"properties":{"frames":{"type":"integer","minimum":0}}}`)},
		{Name: "video.frames", Version: "1.10"},
		{Name: "video.frames", Version: "1.9"},
	}
}

func TestSchemaSubset(t *testing.T) {
	s := frames(t)[0].InputSchema
	cases := []struct {
		name, input, problem string
	}{
		{"valid", `{"source":"https://x","fps":2}`, ""},
		{"missing", `{"source":"https://x"}`, `"fps" is required`},
		{"wrong type", `{"source":"https://x","fps":"2"}`, "$.fps: must be integer, not string"},
		{"not an integer", `{"source":"https://x","fps":1.5}`, "must be integer"},
		{"range", `{"source":"https://x","fps":99}`, "at most 30"},
		{"pattern", `{"source":"ftp://x","fps":1}`, "must match ^https://"},
		{"extra", `{"source":"https://x","fps":1,"mode":"fast"}`, `"mode" is not allowed`},
		{"items", `{"source":"https://x","fps":1,"tags":["a",3]}`, "$.tags[1]: must be string"},
		{"max items", `{"source":"https://x","fps":1,"tags":["a","b","c"]}`, "at most 2 items"},
	}
	for _, c := range cases {
		got := strings.Join(Validate(s, obj(t, c.input)), "; ")
		if c.problem == "" && got != "" || !strings.Contains(got, c.problem) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, got, c.problem)
		}
	}
}

func TestEnumConstNullAndTypeLists(t *testing.T) {
	s := obj(t, `{"type":"object","properties":{"mode":{"enum":["a","b"]},"v":{"const":1},"n":{"type":["string","null"]}}}`)
	if p := Validate(s, obj(t, `{"mode":"a","v":1,"n":null}`)); len(p) != 0 {
		t.Fatalf("valid document refused: %v", p)
	}
	for _, bad := range []string{`{"mode":"c"}`, `{"v":2}`, `{"n":5}`} {
		if len(Validate(s, obj(t, bad))) == 0 {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestUnsupportedSchemasAreRefusedAtLoad(t *testing.T) {
	for name, schema := range map[string]string{
		"oneOf":       `{"oneOf":[{"type":"string"}]}`,
		"ref":         `{"properties":{"a":{"$ref":"#/x"}}}`,
		"nested":      `{"items":{"format":"uri"}}`,
		"bad type":    `{"type":"text"}`,
		"bad pattern": `{"pattern":"("}`,
	} {
		_, err := New([]Type{{Name: "x", Version: "1", InputSchema: obj(t, schema)}}, false)
		if err == nil {
			t.Errorf("%s: an unsupported schema was accepted", name)
		}
	}
}

func TestRegistrationRules(t *testing.T) {
	for name, types := range map[string][]Type{
		"duplicate":   {{Name: "a", Version: "1"}, {Name: "a", Version: "1"}},
		"bad version": {{Name: "a", Version: "v1"}},
		"untyped":     {{Name: "a", Version: "0"}},
		"at in name":  {{Name: "a@1", Version: "1"}},
		"empty name":  {{Version: "1"}},
	} {
		if _, err := New(types, false); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestResolve(t *testing.T) {
	r, err := New(frames(t), false)
	if err != nil {
		t.Fatal(err)
	}
	// newest by number, not by text: 1.10 beats 1.9
	if _, v, _, _ := r.Resolve("video.frames", ""); v != "1.10" {
		t.Errorf("newest = %s, want 1.10", v)
	}
	if n, v, ty, err := r.Resolve("video.frames@1.2", ""); err != nil || n != "video.frames" || v != "1.2" || ty == nil {
		t.Errorf("name@version: %s %s %v %v", n, v, ty, err)
	}
	if _, v, _, err := r.Resolve("video.frames", "1.9"); err != nil || v != "1.9" {
		t.Errorf("separate version: %s %v", v, err)
	}
	if _, _, _, err := r.Resolve("video.frames@1.2", "1.9"); !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("disagreeing versions: %v", err)
	}
	if _, _, _, err := r.Resolve("video.frames@2", ""); !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("unknown version: %v", err)
	}
	// grandfathered: not registered, not strict: accepted as version 0, no contract
	if n, v, ty, err := r.Resolve("ocr.page", ""); err != nil || n != "ocr.page" || v != Untyped || ty != nil {
		t.Errorf("untyped: %s %s %v %v", n, v, ty, err)
	}
	if _, v, _, err := r.Resolve("ocr.page@0", ""); err != nil || v != Untyped {
		t.Errorf("explicit @0: %s %v", v, err)
	}
	if _, _, _, err := r.Resolve("ocr.page@3", ""); !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("a version of an unregistered type: %v", err)
	}
}

func TestStrictRefusesUnknownTypes(t *testing.T) {
	r, _ := New(frames(t), true)
	if _, _, _, err := r.Resolve("ocr.page", ""); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("strict registry accepted an unknown type: %v", err)
	}
	if _, _, _, err := r.Resolve("video.frames", ""); err != nil {
		t.Fatal(err)
	}
}

func TestNilAndEmptyRegistryChangeNothing(t *testing.T) {
	var r *Registry
	if n, v, ty, err := r.Resolve("anything", ""); err != nil || n != "anything" || v != Untyped || ty != nil {
		t.Fatalf("nil registry: %s %s %v %v", n, v, ty, err)
	}
	empty, err := Load("", false)
	if err != nil || len(empty.Types()) != 0 {
		t.Fatal("an empty path must give an empty registry")
	}
}

func TestInputAndResultChecks(t *testing.T) {
	r, _ := New(frames(t), false)
	_, _, ty, _ := r.Resolve("video.frames@1.2", "")
	if err := ty.CheckInput(obj(t, `{"source":"https://x","fps":2}`)); err != nil {
		t.Fatal(err)
	}
	err := ty.CheckInput(obj(t, `{"fps":2}`))
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "video.frames@1.2") {
		t.Fatalf("got %v", err)
	}
	if err := ty.CheckResult(obj(t, `{"frames":12}`)); err != nil {
		t.Fatal(err)
	}
	if err := ty.CheckResult(obj(t, `{"frames":-1}`)); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("got %v", err)
	}
	var none *Type
	if none.CheckInput(nil) != nil || none.CheckResult(nil) != nil {
		t.Fatal("an untyped job has no contract to break")
	}
	if r.Lookup("video.frames", "1.2") == nil || r.Lookup("video.frames", "9") != nil {
		t.Fatal("lookup")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "types.json")
	os.WriteFile(good, []byte(`{"job_types":[{"name":"llm.chat","version":"1","input_schema":{"type":"object","required":["messages"]}}]}`), 0o600)
	r, err := Load(good, false)
	if err != nil || len(r.Types()) != 1 {
		t.Fatalf("%v %v", r, err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"job_types":[{"name":"x","version":"1","input_schema":{"allOf":[]}}]}`), 0o600)
	if _, err := Load(bad, false); err == nil {
		t.Fatal("a file with an unsupported schema must not load")
	}
	if _, err := Load(filepath.Join(dir, "missing.json"), false); err == nil {
		t.Fatal("a missing file must be an error: a typo must not silently disable the registry")
	}
}
