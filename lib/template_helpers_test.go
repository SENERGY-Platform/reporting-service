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

package lib

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFixedHelperNamesAreTheFunctionsTheFixedBlockDeclares(t *testing.T) {
	var declared []string
	for _, match := range functionDeclaration.FindAllStringSubmatch(FixedTemplateHelpers, -1) {
		declared = append(declared, match[1])
	}
	want := slices.Clone(FixedTemplateHelperNames)
	slices.Sort(declared)
	slices.Sort(want)
	if !reflect.DeepEqual(declared, want) {
		t.Errorf("declared %v, listed %v, want the same, only declarations become helpers", declared, want)
	}
}

func TestComposeTemplateHelpersPutsTheFixedBlockInFrontOfTheAuthors(t *testing.T) {
	composed := ComposeTemplateHelpers("function mine() {}")
	if !strings.HasPrefix(composed, FixedTemplateHelpers) || !strings.HasSuffix(composed, "\nfunction mine() {}") {
		t.Errorf("composed = %q, want the fixed block, then the author's text", composed)
	}
	if got := AuthorTemplateHelpers(composed); got != "function mine() {}" {
		t.Errorf("author helpers = %q, want the text without the fixed block", got)
	}
}

func TestComposeTemplateHelpersDoesNotDoubleTheFixedBlock(t *testing.T) {
	once := ComposeTemplateHelpers("function mine() {}")
	if twice := ComposeTemplateHelpers(once); twice != once {
		t.Errorf("composing a composed text changed it:\n%q\n%q", once, twice)
	}
	if got := ComposeTemplateHelpers(""); got != FixedTemplateHelpers+"\n" || AuthorTemplateHelpers(got) != "" {
		t.Errorf("without author helpers the text is the fixed block, got %q", got)
	}
}

func TestAuthorTemplateHelpersLeavesATextWithoutTheFixedBlockAlone(t *testing.T) {
	for _, stored := range []string{"", "function up(s) { return s }", fixedHelpersBegin + "\nno end marker"} {
		if got := AuthorTemplateHelpers(stored); got != stored {
			t.Errorf("AuthorTemplateHelpers(%q) = %q, want it unchanged", stored, got)
		}
	}
}

func TestAuthorTemplateHelpersDropsAFixedBlockOfAnotherVersion(t *testing.T) {
	stored := fixedHelpersBegin + "\nfunction number() {}\n" + fixedHelpersEnd + "\nfunction mine() {}"
	if got := AuthorTemplateHelpers(stored); got != "function mine() {}" {
		t.Errorf("author helpers = %q, want the block cut by its markers", got)
	}
}

func TestCheckAuthorHelpers(t *testing.T) {
	for _, name := range FixedTemplateHelperNames {
		for _, text := range []string{"function " + name + "(v) { return v }", "async function  " + name + " (v) {}", "x\nfunction*" + name + "(){}"} {
			if err := CheckAuthorHelpers(text); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("CheckAuthorHelpers(%q) = %v, want a refusal naming %s", text, err, name)
			}
		}
	}
	for _, text := range []string{"", "function numbers(v) {}", "function indentedName(d) {}", "const number = 1", "function mine() { return number(1) }"} {
		if err := CheckAuthorHelpers(text); err != nil {
			t.Errorf("CheckAuthorHelpers(%q) = %v, want it accepted", text, err)
		}
	}
	if err := CheckAuthorHelpers(ComposeTemplateHelpers("function mine() {}")); err != nil {
		t.Errorf("a text that carries the fixed block is refused: %v", err)
	}
}
