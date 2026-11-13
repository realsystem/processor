// Package jobtypes is the registry of typed job contracts (Autonomy J3): a job type has a name
// ("video.frames"), a version ("1.2") and optional JSON Schemas for its input and result.
//
// The registry is optional and additive. Without a file, or for a type it does not list, jobs
// behave exactly as before and are recorded as version "0" (the grandfathered, untyped
// contract). A listed type is checked when a job is submitted and when a worker returns its
// result. With strict=true, a type that is not listed is refused.
package jobtypes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Untyped is the version of every job whose type is not in the registry.
const Untyped = "0"

var (
	ErrUnknownType    = errors.New("unknown job type")
	ErrUnknownVersion = errors.New("unknown job type version")
	ErrInvalidInput   = errors.New("input does not match the job type")
	ErrInvalidResult  = errors.New("result does not match the job type")
)

var versionRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// Type is one registered contract.
type Type struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"input_schema,omitempty"`
	ResultSchema map[string]interface{} `json:"result_schema,omitempty"`
}

// Registry holds the registered types.
type Registry struct {
	strict bool
	byName map[string][]*Type // newest version first
}

// New builds a registry, refusing duplicates, bad versions and unsupported schemas.
func New(types []Type, strict bool) (*Registry, error) {
	r := &Registry{strict: strict, byName: map[string][]*Type{}}
	seen := map[string]bool{}
	for i := range types {
		t := types[i]
		switch {
		case t.Name == "" || strings.Contains(t.Name, "@") || strings.ContainsAny(t.Name, " \t\n"):
			return nil, fmt.Errorf("job type %q: the name must not be empty or contain '@' or spaces", t.Name)
		case !versionRE.MatchString(t.Version) || t.Version == Untyped:
			return nil, fmt.Errorf("job type %s: version %q must be like 1 or 1.2 (0 is the untyped version)", t.Name, t.Version)
		case seen[t.Name+"@"+t.Version]:
			return nil, fmt.Errorf("job type %s@%s is listed twice", t.Name, t.Version)
		}
		seen[t.Name+"@"+t.Version] = true
		for what, schema := range map[string]map[string]interface{}{"input_schema": t.InputSchema, "result_schema": t.ResultSchema} {
			if schema == nil {
				continue
			}
			if err := CheckSchema(schema); err != nil {
				return nil, fmt.Errorf("job type %s@%s %s: %w", t.Name, t.Version, what, err)
			}
		}
		r.byName[t.Name] = append(r.byName[t.Name], &t)
	}
	for _, list := range r.byName {
		sort.Slice(list, func(i, j int) bool { return compareVersions(list[i].Version, list[j].Version) > 0 })
	}
	return r, nil
}

// Load reads {"job_types": [...]} from a file. An empty path gives an empty registry.
func Load(path string, strict bool) (*Registry, error) {
	if path == "" {
		return New(nil, strict)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("job types file: %w", err)
	}
	var doc struct {
		JobTypes []Type `json:"job_types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("job types file %s: %w", path, err)
	}
	return New(doc.JobTypes, strict)
}

// Strict reports whether unlisted types are refused.
func (r *Registry) Strict() bool { return r != nil && r.strict }

// Types lists every registered type, by name and then newest version first.
func (r *Registry) Types() []*Type {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []*Type
	for _, n := range names {
		out = append(out, r.byName[n]...)
	}
	return out
}

// Resolve turns what a submitter wrote ("name", "name@1.2", or name plus a separate version)
// into the stored name and version and the contract, which is nil for an untyped job.
func (r *Registry) Resolve(spec, version string) (name, ver string, t *Type, err error) {
	name = spec
	if i := strings.Index(spec, "@"); i >= 0 {
		name = spec[:i]
		inline := spec[i+1:]
		if version != "" && version != inline {
			return "", "", nil, fmt.Errorf("%w: %q and version %q disagree", ErrUnknownVersion, spec, version)
		}
		version = inline
	}
	if name == "" {
		return "", "", nil, fmt.Errorf("%w: empty name", ErrUnknownType)
	}
	var list []*Type
	if r != nil {
		list = r.byName[name]
	}
	if len(list) == 0 {
		switch {
		case r.Strict():
			return "", "", nil, fmt.Errorf("%w: %s is not registered", ErrUnknownType, name)
		case version != "" && version != Untyped:
			return "", "", nil, fmt.Errorf("%w: %s has no registered version %s", ErrUnknownVersion, name, version)
		}
		return name, Untyped, nil, nil
	}
	if version == "" {
		return name, list[0].Version, list[0], nil // the newest
	}
	for _, c := range list {
		if c.Version == version {
			return name, version, c, nil
		}
	}
	return "", "", nil, fmt.Errorf("%w: %s has no version %s (registered: %s)", ErrUnknownVersion, name, version, versionsOf(list))
}

// Lookup is Resolve for a stored job: the contract of name@version, or nil.
func (r *Registry) Lookup(name, version string) *Type {
	if r == nil {
		return nil
	}
	for _, c := range r.byName[name] {
		if c.Version == version {
			return c
		}
	}
	return nil
}

// CheckInput validates a submission's input against the contract (nil contract: accepted).
func (t *Type) CheckInput(input map[string]interface{}) error {
	if t == nil || t.InputSchema == nil {
		return nil
	}
	if problems := Validate(t.InputSchema, input); len(problems) > 0 {
		return fmt.Errorf("%w %s@%s: %s", ErrInvalidInput, t.Name, t.Version, strings.Join(first(problems, 5), "; "))
	}
	return nil
}

// CheckResult validates a worker's result against the contract (nil contract: accepted).
func (t *Type) CheckResult(result map[string]interface{}) error {
	if t == nil || t.ResultSchema == nil {
		return nil
	}
	if problems := Validate(t.ResultSchema, result); len(problems) > 0 {
		return fmt.Errorf("%w %s@%s: %s", ErrInvalidResult, t.Name, t.Version, strings.Join(first(problems, 5), "; "))
	}
	return nil
}

func first(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("(%d more)", len(s)-n))
	}
	return s
}

func versionsOf(list []*Type) string {
	v := make([]string, len(list))
	for i, t := range list {
		v[i] = t.Version
	}
	return strings.Join(v, ", ")
}

func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}
