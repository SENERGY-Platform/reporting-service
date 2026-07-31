/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package reportsync

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Kind is one syncable property of one jsreport collection. Only these
// properties are ever read or written, everything else - engine, recipe, the
// links between template, script and data, permissions - stays with the
// instance, because those references use shortids that differ per store.
//
// The file name of an entity is built from its name, the marker of the kind and
// an extension, so a template with a helpers property and an asset called
// "helpers.js" cannot collide:
//
//	device_state.template.handlebars   template content
//	device_state.template.helpers.js   template helpers
//	device-state.script.js             script "device-state.js"
//	device-state-data.data.json        data entity
//	helpers.asset.js                   asset "helpers.js"
type Kind struct {
	Collection string
	Property   string
	// Marker identifies the kind inside the file name.
	Marker string
	// Extension is used when the entity name carries none of its own.
	Extension string
	// NameCarriesExtension marks collections whose entity names include a file
	// extension, which then moves behind the marker instead of being repeated.
	NameCarriesExtension bool
	// Base64 marks properties that jsreport serves base64 encoded.
	Base64 bool
	// Creatable marks kinds that -create may insert. Templates are never
	// inserted, a template without engine and recipe would be broken.
	Creatable bool
	// Validate rejects a source file before it is written to the instance.
	Validate func(content string) error
}

var Kinds = []Kind{
	{Collection: "templates", Property: "content", Marker: ".template", Extension: ".handlebars"},
	{Collection: "templates", Property: "helpers", Marker: ".template.helpers", Extension: ".js"},
	{Collection: "scripts", Property: "content", Marker: ".script", Extension: ".js",
		NameCarriesExtension: true, Creatable: true},
	{Collection: "data", Property: "dataJson", Marker: ".data", Extension: ".json",
		Creatable: true, Validate: validJson},
	{Collection: "assets", Property: "content", Marker: ".asset", Extension: "",
		NameCarriesExtension: true, Base64: true, Creatable: true},
}

func validJson(content string) error {
	if !json.Valid([]byte(content)) {
		return fmt.Errorf("not valid json")
	}
	return nil
}

// Collections lists the collections that have to be read for a sync run,
// together with the properties needed from each of them.
func Collections() map[string][]string {
	result := map[string][]string{}
	for _, kind := range Kinds {
		result[kind.Collection] = append(result[kind.Collection], kind.Property)
	}
	return result
}

// FileName is the file a given entity of this kind is stored in.
func (k Kind) FileName(entityName string) string {
	if !k.NameCarriesExtension {
		return entityName + k.Marker + k.Extension
	}
	if extension := path.Ext(entityName); extension != "" {
		return strings.TrimSuffix(entityName, extension) + k.Marker + extension
	}
	return entityName + k.Marker
}

// KindOf resolves a file name to its kind and the name of the entity it holds.
func KindOf(file string) (Kind, string, bool) {
	name := path.Base(file)

	// longest marker first, so ".template.helpers" wins over ".template"
	kinds := append([]Kind(nil), Kinds...)
	sort.Slice(kinds, func(i, j int) bool { return len(kinds[i].Marker) > len(kinds[j].Marker) })

	for _, kind := range kinds {
		if kind.NameCarriesExtension {
			continue
		}
		suffix := kind.Marker + kind.Extension
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return kind, strings.TrimSuffix(name, suffix), true
		}
	}

	for _, kind := range kinds {
		if !kind.NameCarriesExtension {
			continue
		}
		if strings.HasSuffix(name, kind.Marker) && len(name) > len(kind.Marker) {
			return kind, strings.TrimSuffix(name, kind.Marker), true
		}
		if index := strings.LastIndex(name, kind.Marker+"."); index > 0 {
			return kind, name[:index] + name[index+len(kind.Marker):], true
		}
	}
	return Kind{}, "", false
}

func kindFor(collection string, property string) (Kind, bool) {
	for _, kind := range Kinds {
		if kind.Collection == collection && kind.Property == property {
			return kind, true
		}
	}
	return Kind{}, false
}

// decode turns the value as served by jsreport into the file content.
func (k Kind) decode(value string) (string, error) {
	if !k.Base64 {
		return value, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("could not decode %s content: %w", k.Collection, err)
	}
	return string(decoded), nil
}

// encode turns the file content into the value jsreport expects.
func (k Kind) encode(content string) string {
	if !k.Base64 {
		return content
	}
	return base64.StdEncoding.EncodeToString([]byte(content))
}

func (k Kind) String() string {
	return k.Collection + "." + k.Property
}
