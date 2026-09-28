package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/hoisie/mustache"
	"github.com/iancoleman/strcase"
	"github.com/nasa9084/go-openapi"
)

//go:embed client.go.mustache
var clientTemplate string

func main() {
	resp, err := http.Get("https://raw.githubusercontent.com/cloud-hypervisor/cloud-hypervisor/v53.0/vmm/src/api/openapi/cloud-hypervisor.yaml")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	spec, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	doc, err := openapi.Load(spec)
	if err != nil {
		panic(err)
	}

	var eps []endpoint
	for path, pathItem := range doc.Paths {
		eps = append(eps, *newEndpoint(path, pathItem))
	}
	sort.Sort(endpointSorter(eps))

	for name, schema := range doc.Components.Schemas {
		if schema.Type == "string" {
			stringTypes[name] = true
		}
	}

	var tps []type_
	for name, schema := range doc.Components.Schemas {
		tps = append(tps, *newType(name, schema))
	}
	sort.Sort(typeSorter(tps))

	encoded, err := json.Marshal(map[string]interface{}{
		"endpoints": eps,
		"types":     tps,
	})
	if err != nil {
		panic(err)
	}

	var data interface{}
	if err := json.Unmarshal(encoded, &data); err != nil {
		panic(err)
	}

	fmt.Println(mustache.Render(clientTemplate, data))
}

type endpoint struct {
	Name   string `json:"name,omitempty"`
	Desc   string `json:"desc,omitempty"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Arg    string `json:"arg,omitempty"`
	Ret    string `json:"ret,omitempty"`
}

func newEndpoint(path string, pathItem *openapi.PathItem) *endpoint {
	ep := &endpoint{
		Name: strings.Title(strcase.ToCamel(path)),
		Desc: pathItem.Description,
		Path: path,
	}

	var op *openapi.Operation
	switch {
	case pathItem.Get != nil:
		ep.Method = "GET"
		op = pathItem.Get
	case pathItem.Put != nil:
		ep.Method = "PUT"
		op = pathItem.Put
	case pathItem.Post != nil:
		ep.Method = "POST"
		op = pathItem.Post
	case pathItem.Delete != nil:
		ep.Method = "DELETE"
		op = pathItem.Delete
	case pathItem.Patch != nil:
		ep.Method = "PATCH"
		op = pathItem.Patch
	default:
		panic(nil)
	}

	if ep.Desc == "" {
		ep.Desc = op.Summary
	}

	if op.RequestBody != nil {
		if arg := op.RequestBody.Content["application/json"]; arg != nil {
			ep.Arg = schemaToTypeName(arg.Schema)
		}
	}

	for code, resp := range op.Responses {
		if strings.HasPrefix(code, "2") {
			if ret := resp.Content["application/json"]; ret != nil {
				ep.Ret = schemaToTypeName(ret.Schema)
			}
		}
	}

	return ep
}

// stringTypes holds the names of string (enum) schemas, which are generated
// as named string types and referenced by value instead of by pointer.
var stringTypes = map[string]bool{}

type type_ struct {
	Name     string      `json:"name,omitempty"`
	Desc     string      `json:"desc,omitempty"`
	IsString bool        `json:"isString,omitempty"`
	HasEnums bool        `json:"hasEnums,omitempty"`
	Enums    []enumValue `json:"enums,omitempty"`
	Fields   []field     `json:"fields,omitempty"`
}

type enumValue struct {
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
}

func newType(name string, schema *openapi.Schema) *type_ {
	tp := &type_{
		Name:     name,
		Desc:     strings.TrimSpace(schema.Description),
		IsString: schema.Type == "string",
	}
	for _, value := range schema.Enum {
		tp.Enums = append(tp.Enums, enumValue{
			Name:  name + strcase.ToCamel(value),
			Type:  name,
			Value: value,
		})
	}
	tp.HasEnums = len(tp.Enums) > 0
	for fieldName, fieldSchema := range schema.Properties {
		var required bool
		for _, requiredFieldName := range schema.Required {
			if requiredFieldName == fieldName {
				required = true
			}
		}
		tp.Fields = append(tp.Fields, *newField(fieldName, fieldSchema, required))
	}
	sort.Sort(fieldSorter(tp.Fields))
	return tp
}

type field struct {
	Name     string `json:"name,omitempty"`
	Desc     string `json:"desc,omitempty"`
	Type     string `json:"type,omitempty"`
	Key      string `json:"key,omitempty"`
	Required bool   `json:"required,omitempty"`
}

func newField(key string, schema *openapi.Schema, required bool) *field {
	return &field{
		Name:     strings.Title(strcase.ToCamel(key)),
		Desc:     schema.Description,
		Type:     schemaToTypeName(schema),
		Key:      key,
		Required: required,
	}
}

func schemaToTypeName(schema *openapi.Schema) string {
	if schema.Ref != "" {
		segs := strings.Split(schema.Ref, "/")
		name := segs[len(segs)-1]
		if stringTypes[name] {
			return name
		}
		return "*" + name
	}

	switch schema.Type {
	case "boolean":
		return "bool"
	case "integer":
		switch schema.Format {
		case "int16":
			return "int16"
		case "int64":
			return "int64"
		default:
			return "int"
		}
	case "string":
		return "string"
	case "array":
		return "[]" + schemaToTypeName(schema.Items)
	case "object":
		if schema.AdditionalProperties == nil {
			return "map[string]interface{}"
		}
		return "map[string]" + schemaToTypeName(schema.AdditionalProperties)
	default:
		panic(nil)
	}
}

type endpointSorter []endpoint

func (s endpointSorter) Len() int           { return len(s) }
func (s endpointSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s endpointSorter) Less(i, j int) bool { return s[i].Name < s[j].Name }

type typeSorter []type_

func (s typeSorter) Len() int           { return len(s) }
func (s typeSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s typeSorter) Less(i, j int) bool { return s[i].Name < s[j].Name }

type fieldSorter []field

func (s fieldSorter) Len() int           { return len(s) }
func (s fieldSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s fieldSorter) Less(i, j int) bool { return s[i].Name < s[j].Name }
