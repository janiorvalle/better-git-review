package changegraph

import (
	"slices"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestBuildResolvesTypeScriptRelativeAndIndexImports(t *testing.T) {
	files := []document.File{
		testFile("src/z-definition.ts", `export function calculateTotal() { return 1 }`),
		testFile("src/a-consumer.ts", `import { calculateTotal } from "./z-definition"`, `calculateTotal()`),
		testFile("src/lib/index.ts", `export const sharedValue = 1`),
		testFile("src/b-index-consumer.ts", `const lib = require("./lib")`),
		testFile("src/reexport.ts", `export { sharedValue } from "./lib"`),
		testFile("src/extension-consumer.ts", `import { calculateTotal } from "./z-definition.js"`),
	}
	want := []Edge{
		{Importer: 1, Imported: 0},
		{Importer: 3, Imported: 2},
		{Importer: 4, Imported: 2},
		{Importer: 5, Imported: 0},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesModernTypeScriptModuleExtensions(t *testing.T) {
	files := []document.File{
		testFile("src/helper.mts", `export const abc = 1`),
		testFile("src/consumer.cts", `import { abc } from "./helper.mjs"`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDropsAmbiguousExactModuleAlias(t *testing.T) {
	files := []document.File{
		testFile("src/lib.ts", `export const abc = 1`),
		testFile("src/lib/index.ts", `export const abc = 2`),
		testFile("src/consumer.ts", `import { abc } from "./lib"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("ambiguous exact alias produced edges: %#v", got)
	}
}

func TestBuildResolvesMultilineTypeScriptImportAndIgnoresCommentOnlyImport(t *testing.T) {
	files := []document.File{
		testFile("src/helper.ts", `export const abc = 1`),
		testFile("src/consumer.ts", `import {`, `  abc`, `} from "./helper"`),
		testFile("src/comment.ts", `// import { abc } from "./helper"`,
			`// const fake = require("./helper")`, `const note = 'require("./helper")'`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDoesNotLeakSemicolonlessJSImportState(t *testing.T) {
	files := []document.File{
		testFile("src/helper.ts", `export const abc = 1`),
		testFile("src/polyfill.ts", `export const xyz = 1`),
		testFile("src/side-effect.ts", `import "./polyfill"`, `const note = 'from "./helper"'`),
		testFile("src/local-export.ts", `export { abc }`, `const note = 'from "./helper"'`),
	}
	want := []Edge{{Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want side-effect edge without leaked parser state %#v", got, want)
	}
}

func TestBuildIgnoresSideEffectImportsInBlockComments(t *testing.T) {
	files := []document.File{
		testFile("src/polyfill.ts", `export const value = 1`),
		testFile("src/app.ts", `/*`, `import "./polyfill"`, `*/`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("commented side-effect import produced edges: %#v", got)
	}
}

func TestBuildJSImportCommentStrippingPreservesRegexLiterals(t *testing.T) {
	files := []document.File{
		testFile("src/dep.ts", `export const value = 1`),
		testFile("src/app.ts", `const pattern = /[/*]/;`, `const dep = require("./dep")`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesMultilineSideEffectImports(t *testing.T) {
	files := []document.File{
		testFile("src/styles.css", `body { color: black; }`),
		testFile("src/app.ts", `import`, `  "./styles.css";`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesGoPackageImport(t *testing.T) {
	files := []document.File{
		testFile("internal/money/money.go", `package money`, `func CalculateTotal() int { return 1 }`),
		testFile("cmd/app/main.go", `package main`, `import "github.com/acme/project/internal/money"`, `var total = money.CalculateTotal()`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDropsAmbiguousRepositorySuffix(t *testing.T) {
	files := []document.File{
		testFile("one/money/money.go", `package money`),
		testFile("two/money/money.go", `package money`),
		testFile("cmd/app/main.go", `package main`, `import "money"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("ambiguous import produced edges: %#v", got)
	}
}

func TestBuildResolvesPythonModule(t *testing.T) {
	files := []document.File{
		testFile("pkg/money.py", `def calculate_total():`, `    return 1`),
		testFile("pkg/service.py", `from .money import calculate_total`, `value = calculate_total()`),
		testFile("pkg/runner.py", `import pkg.money`),
	}
	want := []Edge{{Importer: 1, Imported: 0}, {Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesEveryCommaSeparatedPythonImport(t *testing.T) {
	files := []document.File{
		testFile("pkg/alpha.py", `value = 1`),
		testFile("pkg/beta.py", `value = 2`),
		testFile("pkg/consumer.py", `import pkg.alpha, pkg.beta as beta`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildRejectsRelativeImportOutsideRepository(t *testing.T) {
	files := []document.File{
		testFile("shared.ts", `export const abc = 1`),
		testFile("src/consumer.ts", `import { abc } from "../../shared"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("out-of-repository import produced edges: %#v", got)
	}
}

func TestBuildRejectsPythonRelativeImportAboveRepository(t *testing.T) {
	files := []document.File{
		testFile("shared.py", `value = 1`),
		testFile("service.py", `from ..shared import value`),
		testFile("pkg/service.py", `from ...shared import value`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("out-of-repository Python import produced edges: %#v", got)
	}
}

func TestBuildAllowsPythonRelativeImportToRepositoryRoot(t *testing.T) {
	files := []document.File{
		testFile("shared.py", `value = 1`),
		testFile("pkg/service.py", `from ..shared import value`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesJavaAndKotlinImports(t *testing.T) {
	files := []document.File{
		testFile("src/main/java/com/acme/model/Money.java", `package com.acme.model;`),
		testFile("src/main/java/com/acme/service/MoneyService.java", `import com.acme.model.Money;`),
		testFile("src/main/java/com/acme/util/Amounts.java", `package com.acme.util;`),
		testFile("src/main/java/com/acme/app/Application.java", `import static com.acme.util.Amounts.round;`),
		testFile("src/main/kotlin/com/acme/view/Labels.kt", `package com.acme.view`),
		testFile("src/main/kotlin/com/acme/view/Page.kts", `import com.acme.view.Labels as ViewLabels`),
	}
	want := []Edge{
		{Importer: 1, Imported: 0},
		{Importer: 3, Imported: 2},
		{Importer: 5, Imported: 4},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesJVMWildcardImportsToEveryChangedPackageFile(t *testing.T) {
	files := []document.File{
		testFile("src/main/java/com/acme/parts/Alpha.java", `package com.acme.parts;`),
		testFile("src/main/kotlin/com/acme/parts/Beta.kt", `package com.acme.parts`),
		testFile("src/main/java/com/acme/app/Application.java", `import com.acme.parts.*;`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesJavaStaticWildcardToOwningClass(t *testing.T) {
	files := []document.File{
		testFile("src/main/java/com/acme/util/Amounts.java", `package com.acme.util;`),
		testFile("src/main/java/com/acme/app/Application.java", `import static com.acme.util.Amounts.*;`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildFindsJVMDefinitionsAndSamePackageReferences(t *testing.T) {
	files := []document.File{
		testFile("java/ZHelper.java", `public final class JavaHelper {}`),
		testFile("java/AConsumer.java", `JavaHelper helper;`),
		testFile("kotlin/Definitions.kt", `fun calculateTotal() = 1`),
		testFile("kotlin/Consumer.kt", `val total = calculateTotal()`),
	}
	want := []Edge{{Importer: 1, Imported: 0}, {Importer: 3, Imported: 2}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildJVMRequiresOneDefiningFileAndFourCharacterName(t *testing.T) {
	files := []document.File{
		testFile("one/Shared.java", `public class SharedName {}`, `class Foo {}`),
		testFile("two/Shared.kt", `class SharedName`),
		testFile("consumer/Use.java", `SharedName value;`, `Foo shortName;`),
		testFile("unique/Only.kt", `object UniqueObject`),
		testFile("consumer/Unique.java", `UniqueObject value;`),
	}
	want := []Edge{{Importer: 4, Imported: 3}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDoesNotMisreadUnsupportedKotlinFunctionForms(t *testing.T) {
	files := []document.File{
		testFile("kotlin/Extensions.kt", `fun Order.calculateTotal() = 1`, `fun <T> genericTotal() = 1`),
		testFile("kotlin/Consumer.kt", `val order = Order()`, `val total = calculateTotal()`, `genericTotal<String>()`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("unsupported Kotlin function form produced edges: %#v", got)
	}
}

func TestBuildSupportsTrailingDollarJavaIdentifier(t *testing.T) {
	files := []document.File{
		testFile("java/Money.java", `public class Money$ {}`),
		testFile("java/Consumer.java", `Money$ amount;`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesDirectoryScopedTerraformReferences(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "x" {}`),
		testFile("stack/locals.tf", `locals {`, `  prefix = "app"`, `}`),
		testFile("stack/modules.tf", `module "network" {`, `  source = "hashicorp/consul/aws"`, `}`),
		testFile("stack/resources.tf", `resource "aws_s3_bucket" "assets" {}`, `data "aws_caller_identity" "current" {}`),
		testFile("stack/a-consumer.tf",
			`output "value" { value = var.x }`,
			`output "prefix" { value = local.prefix }`,
			`output "module" { value = module.network.id }`,
			`output "bucket" { value = aws_s3_bucket.assets.id }`,
			`output "account" { value = data.aws_caller_identity.current.account_id }`),
	}
	want := []Edge{
		{Importer: 4, Imported: 0},
		{Importer: 4, Imported: 1},
		{Importer: 4, Imported: 2},
		{Importer: 4, Imported: 3},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformDefinitionsMustBeUniqueWithinDirectory(t *testing.T) {
	files := []document.File{
		testFile("stack/one.tf", `variable "region" {}`),
		testFile("stack/two.tf", `variable "region" {}`),
		testFile("stack/short.tf", `variable "x" {}`),
		testFile("stack/consumer.tf", `value = var.region`, `short = var.x`),
		testFile("other/variables.tf", `variable "region" {}`),
		testFile("other/consumer.tf", `value = var.region`),
	}
	want := []Edge{{Importer: 3, Imported: 2}, {Importer: 5, Imported: 4}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformReferencesDoNotCrossDirectories(t *testing.T) {
	files := []document.File{
		testFile("one/variables.tf", `variable "region" {}`),
		testFile("two/consumer.tf", `value = var.region`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("directory-scoped reference produced cross-directory edges: %#v", got)
	}
}

func TestBuildResolvesRelativeTerraformModuleSources(t *testing.T) {
	files := []document.File{
		testFile("modules/network/main.tf", `resource "aws_vpc" "main" {}`),
		testFile("modules/network/variables.tf", `variable "cidr" {}`),
		testFile("stacks/dev/main.tf", `module "network" {`, `  source = "../../modules/network"`, `}`),
		testFile("stacks/dev/remote.tf", `module "remote" {`, `  source = "hashicorp/consul/aws"`, `}`),
		testFile("stacks/dev/not-module.tf", `terraform {`, `  source = "../../modules/network"`, `}`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformModuleSourceResolvesRepositoryRoot(t *testing.T) {
	files := []document.File{
		testFile("main.tf", `resource "null_resource" "root" {}`),
		testFile("examples/demo/main.tf", `module "root" { source = "../.." }`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesInlineTerraformModuleSource(t *testing.T) {
	files := []document.File{
		testFile("modules/network/main.tf", `resource "aws_vpc" "main" {}`),
		testFile("stack/main.tf", `module "network" { source = "../modules/network" }`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildInlineTerraformModuleSourceMustBeDirect(t *testing.T) {
	files := []document.File{
		testFile("fake/main.tf", `resource "null_resource" "fake" {}`),
		testFile("stack/main.tf", `module "remote" { config = { source = "../fake" }; source = "hashicorp/consul/aws" }`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("nested source became a module source: %#v", got)
	}
}

func TestBuildTerraformIgnoresCommentsAndLiteralStringsButKeepsTemplates(t *testing.T) {
	files := []document.File{
		testFile("stack/resources.tf", `resource "aws_s3_bucket" "assets" {}`),
		testFile("stack/false.tf",
			`description = "aws_s3_bucket.assets"`,
			`description = "$${aws_s3_bucket.assets.id}"`,
			`description = "${format("aws_s3_bucket.assets.id")}"`,
			`# aws_s3_bucket.assets.id`,
			`/*`, `aws_s3_bucket.assets.id`, `*/`),
		testFile("stack/template.tf", `value = "${aws_s3_bucket.assets.id}"`),
	}
	want := []Edge{{Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformIgnoresCommentsInsideTemplates(t *testing.T) {
	files := []document.File{
		testFile("stack/resources.tf", `resource "aws_s3_bucket" "assets" {}`),
		testFile("stack/variables.tf", `variable "real" {}`),
		testFile("stack/main.tf", `value = "${ /* aws_s3_bucket.assets.id */ var.real }"`),
	}
	want := []Edge{{Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformStateResetsAcrossAddedLineGaps(t *testing.T) {
	files := []document.File{
		testFile("modules/network/main.tf", `resource "aws_vpc" "main" {}`),
		{
			Path: "stack/main.tf", Additions: 2,
			Hunks: []document.Hunk{
				{Lines: []document.HunkLine{
					{Type: "a", New: 1, Text: `module "network" {`},
					{Type: "c", New: 2, Text: `}`},
				}},
				{Lines: []document.HunkLine{{Type: "a", New: 10, Text: `source = "../modules/network"`}}},
			},
		},
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("Terraform parser state crossed an added-line gap: %#v", got)
	}
}

func TestBuildTerraformHeredocStateResetsAcrossContext(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "region" {}`),
		{
			Path: "stack/main.tf", Additions: 2,
			Hunks: []document.Hunk{{Lines: []document.HunkLine{
				{Type: "a", New: 1, Text: `description = <<EOT`},
				{Type: "c", New: 2, Text: `EOT`},
				{Type: "a", New: 3, Text: `value = var.region`},
			}}},
		},
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformReservedNamespacesAreNotResources(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "region" {}`),
		testFile("stack/resources.tf", `resource "var" "region" {}`),
		testFile("stack/main.tf", `value = var.region`),
	}
	want := []Edge{{Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformNamespacesOnlyMatchRootTraversals(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "id" {}`),
		testFile("stack/resources.tf", `resource "aws_instance" "var" {}`),
		testFile("stack/main.tf", `value = aws_instance.var.id`),
	}
	want := []Edge{{Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformFindsAdjacentReferencesOfTheSameKind(t *testing.T) {
	files := []document.File{
		testFile("stack/a.tf", `variable "a" {}`),
		testFile("stack/b.tf", `variable "b" {}`),
		testFile("stack/main.tf", `value = [var.a,var.b]`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformHeredocsOnlyResolveInterpolations(t *testing.T) {
	files := []document.File{
		testFile("stack/resources.tf", `resource "aws_s3_bucket" "assets" {}`),
		testFile("stack/literal.tf",
			`description = <<EOT`,
			`aws_s3_bucket.assets.id`,
			`$${aws_s3_bucket.assets.id}`,
			`EOT`),
		testFile("stack/template.tf",
			`description = <<-EOT`,
			`bucket = ${aws_s3_bucket.assets.id}`,
			`EOT`),
	}
	want := []Edge{{Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformRecognizesHeredocsInNestedExpressions(t *testing.T) {
	files := []document.File{
		testFile("stack/resources.tf", `resource "aws_s3_bucket" "assets" {}`),
		testFile("stack/main.tf",
			`values = [<<EOT`,
			`aws_s3_bucket.assets.id`,
			`EOT`,
			`]`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("nested heredoc literal produced edges: %#v", got)
	}
}

func TestBuildTerraformTemplateDirectivesResolveReferences(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "enabled" {}`),
		testFile("stack/quoted.tf", `description = "%{ if var.enabled }yes%{ endif }"`),
		testFile("stack/heredoc.tf",
			`description = <<EOT`,
			`%{ if var.enabled }yes%{ endif }`,
			`%%{ if var.disabled }literal%{ endif }`,
			`EOT`),
	}
	want := []Edge{{Importer: 1, Imported: 0}, {Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformHeredocTemplatesIgnoreQuotedBraces(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "parts" {}`),
		testFile("stack/main.tf",
			`description = <<EOT`,
			`${join("}", var.parts)}`,
			`EOT`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformPreservesNestedStringTemplates(t *testing.T) {
	files := []document.File{
		testFile("stack/variables.tf", `variable "name" {}`),
		testFile("stack/main.tf", `value = "${format("prefix-${var.name}")}"`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformFindsInlineLocalAssignments(t *testing.T) {
	files := []document.File{
		testFile("stack/locals.tf", `locals { p = "app" config = { nested = "ignored" } }`),
		testFile("stack/consumer.tf", `value = local.p`),
		testFile("stack/nested-consumer.tf", `value = local.nested`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildTerraformDoesNotTreatEqualityAsLocalAssignment(t *testing.T) {
	files := []document.File{
		testFile("stack/locals.tf", `locals { enabled = var.flag == local.other }`),
		testFile("stack/consumer.tf", `value = local.flag`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("equality operand became a local definition: %#v", got)
	}
}

func TestBuildExplicitExtensionsResolveAcrossLanguages(t *testing.T) {
	files := []document.File{
		testFile("src/styles.css", `.app { color: red; }`),
		testFile("src/logo.svg", `<svg></svg>`),
		testFile("src/app.ts", `import "./styles.css"`, `const logo = require("./logo.svg")`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildExplicitExtensionPrefersExactPathOverAlias(t *testing.T) {
	files := []document.File{
		testFile("src/data.json", `{}`),
		testFile("src/data.json.ts", `const internalValue = {}`),
		testFile("src/app.ts", `const payload = require("./data.json")`),
	}
	want := []Edge{{Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildExplicitExtensionPrefersLocalTranspilationAliasOverSuffix(t *testing.T) {
	files := []document.File{
		testFile("src/foo.ts", `export const value = 1`),
		testFile("vendor/src/foo.js", `export const value = 2`),
		testFile("src/app.ts", `import { value } from "./foo.js"`),
	}
	want := []Edge{{Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildExplicitExtensionKeepsTranspilationFallbackLanguageFiltered(t *testing.T) {
	files := []document.File{
		testFile("src/theme.css", `body { color: black; }`),
		testFile("src/app.ts", `import "./theme.js"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("transpilation fallback crossed languages: %#v", got)
	}
}

func TestBuildRelativeExplicitExtensionDoesNotUseRepositorySuffix(t *testing.T) {
	files := []document.File{
		testFile("vendor/src/logo.svg", `<svg></svg>`),
		testFile("src/app.ts", `import logo from "./logo.svg"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("relative import escaped its directory: %#v", got)
	}
}

func TestBuildResolvesEveryReactNativePlatformVariant(t *testing.T) {
	files := []document.File{
		testFile("src/Button.tsx", `export const Button = 0`),
		testFile("src/Button.ios.tsx", `export const Button = 1`),
		testFile("src/Button.android.tsx", `export const Button = 2`),
		testFile("src/Button.native.js", `export const Button = 3`),
		testFile("src/Button.web.ts", `export const Button = 4`),
		testFile("src/Screen.tsx", `import { Button } from "./Button"`),
	}
	want := []Edge{
		{Importer: 5, Imported: 0},
		{Importer: 5, Imported: 1},
		{Importer: 5, Imported: 2},
		{Importer: 5, Imported: 3},
		{Importer: 5, Imported: 4},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesPlatformVariantsFromSideEffectImport(t *testing.T) {
	files := []document.File{
		testFile("src/Button.ios.tsx", `registerIOS()`),
		testFile("src/Button.android.tsx", `registerAndroid()`),
		testFile("src/Screen.tsx", `import "./Button"`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesPlatformVariantIndexModules(t *testing.T) {
	files := []document.File{
		testFile("src/widgets/index.ts", `export const Widget = 0`),
		testFile("src/widgets/index.ios.tsx", `export const Widget = 1`),
		testFile("src/widgets/index.android.tsx", `export const Widget = 2`),
		testFile("src/Screen.tsx", `import { Widget } from "./widgets"`),
	}
	want := []Edge{{Importer: 3, Imported: 0}, {Importer: 3, Imported: 1}, {Importer: 3, Imported: 2}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesRelativeHTMLAndCSSAssets(t *testing.T) {
	files := []document.File{
		testFile("web/app.js", `console.log("app")`),
		testFile("web/theme.css", `body { color: black; }`),
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/font.woff2", `font-data`),
		testFile("web/index.html",
			`<script src="./app.js"></script><link href='theme.css'><img src=logo.svg>`,
			`<img src="https://example.com/remote.svg"><script src="/root.js"></script>`),
		testFile("web/styles.css",
			`@import "./theme.css";`,
			`@font-face { src: url(font.woff2) }`,
			`.remote { background: url(data:image/png;base64,abc) }`),
	}
	want := []Edge{
		{Importer: 4, Imported: 0},
		{Importer: 4, Imported: 1},
		{Importer: 4, Imported: 2},
		{Importer: 5, Imported: 1},
		{Importer: 5, Imported: 3},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesMultilineCSSAssets(t *testing.T) {
	files := []document.File{
		testFile("web/theme.css", `body { color: black; }`),
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/styles.css",
			`@import`, `  "./theme.css";`,
			`.logo { background: url(`, `  "./logo.svg"`, `) }`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildMultilineCSSDoesNotCrossAddedLineGaps(t *testing.T) {
	files := []document.File{
		testFile("web/theme.css", `body { color: black; }`),
		{
			Path: "web/styles.css", Additions: 2,
			Hunks: []document.Hunk{{Lines: []document.HunkLine{
				{Type: "a", New: 1, Text: `@import`},
				{Type: "c", New: 2, Text: `"existing.css";`},
				{Type: "a", New: 3, Text: `"./theme.css";`},
			}}},
		},
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("CSS parser joined unrelated additions: %#v", got)
	}
}

func TestBuildIgnoresCommentedHTMLAndCSSAssets(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/index.html", `<!-- <img src="./logo.svg"> -->`),
		testFile("web/styles.css", `/* url("./logo.svg") */`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("commented assets produced edges: %#v", got)
	}
}

func TestBuildHTMLAttributesRespectNamesAndQuotedValues(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/index.html",
			`<img alt='src="./logo.svg"'>`,
			`<img data-src="./logo.svg">`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("non-src HTML attributes produced edges: %#v", got)
	}
}

func TestBuildHTMLCommentsAfterTextApostrophesRemainComments(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/index.html", `<p>don't</p><!-- <img src="./logo.svg"> -->`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("comment after text apostrophe produced edges: %#v", got)
	}
}

func TestBuildCSSCommentMarkersInsideStringsRemainLiteral(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/styles.css",
			`.label::before { content: "/*" }`,
			`.logo { background: url("./logo.svg") }`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildIgnoresCSSAssetSyntaxInsideStrings(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/styles.css", `.label::before { content: 'url("./logo.svg")' }`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("CSS string content produced edges: %#v", got)
	}
}

func TestBuildIgnoresTagShapedTextInsideHTMLRawElements(t *testing.T) {
	files := []document.File{
		testFile("web/logo.svg", `<svg></svg>`),
		testFile("web/index.html", `<script>const example = '<img src="./logo.svg">';</script>`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("raw script text produced edges: %#v", got)
	}
}

func TestBuildResolvesMultilineHTMLTag(t *testing.T) {
	files := []document.File{
		testFile("web/app.js", `console.log("app")`),
		testFile("web/index.html", `<script`, `  src="./app.js"`, `></script>`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildMultilineHTMLDoesNotCrossAddedLineGaps(t *testing.T) {
	files := []document.File{
		testFile("web/app.js", `console.log("app")`),
		{
			Path: "web/index.html", Additions: 2,
			Hunks: []document.Hunk{{Lines: []document.HunkLine{
				{Type: "a", New: 1, Text: `<script`},
				{Type: "c", New: 2, Text: `></script>`},
				{Type: "a", New: 3, Text: `src="./app.js"`},
			}}},
		},
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("HTML parser joined unrelated additions: %#v", got)
	}
}

func TestBuildRelativeAssetsDoNotUseRepositorySuffixFallback(t *testing.T) {
	files := []document.File{
		testFile("vendor/web/logo.svg", `<svg></svg>`),
		testFile("web/index.html", `<img src="./logo.svg">`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("relative asset escaped its directory: %#v", got)
	}
}

func TestBuildFindsLanguageSpecificExportedDefinitions(t *testing.T) {
	files := []document.File{
		testFile("go/definition.go", `func GoHelper() {}`),
		testFile("go/consumer.go", `func use() { GoHelper() }`),
		testFile("python/definition.py", `class PythonHelper:`),
		testFile("python/consumer.py", `value = PythonHelper()`),
		testFile("typescript/definition.ts", `export type SharedType = string`),
		testFile("typescript/consumer.ts", `const value: SharedType = "x"`),
	}
	want := []Edge{
		{Importer: 1, Imported: 0},
		{Importer: 3, Imported: 2},
		{Importer: 5, Imported: 4},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildUsesOnlyUniqueSymbolsWithAtLeastFourCharacters(t *testing.T) {
	files := []document.File{
		testFile("a.ts", `export function SharedName() {}`, `export function Foo() {}`),
		testFile("b.ts", `export class SharedName {}`),
		testFile("c.ts", `SharedName()`, `Foo()`),
		testFile("d.ts", `export interface UniqueType { value: string }`),
		testFile("e.ts", `const value: UniqueType = { value: "x" }`),
	}
	want := []Edge{{Importer: 4, Imported: 3}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildIgnoresContextAndDeletedLines(t *testing.T) {
	definition := testFile("src/definition.ts", `export const sharedValue = 1`)
	consumer := document.File{
		Path: "src/consumer.ts",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{
			{Type: "c", Text: `import { sharedValue } from "./definition"`},
			{Type: "d", Text: `sharedValue`},
		}}},
	}
	if got := Build([]document.File{definition, consumer}); len(got) != 0 {
		t.Fatalf("non-added lines produced edges: %#v", got)
	}
}

func TestStableOrderPutsDependenciesFirstAndKeepsEdgeFreeOrder(t *testing.T) {
	if got := StableOrder([]int{0, 1}, []Edge{{Importer: 0, Imported: 1}}); !slices.Equal(got, []int{1, 0}) {
		t.Fatalf("ordered = %#v", got)
	}
	current := []int{3, 1, 2}
	if got := StableOrder(current, nil); !slices.Equal(got, current) {
		t.Fatalf("edge-free order changed: %#v", got)
	}
}

func TestStableOrderDropsCycleEdges(t *testing.T) {
	current := []int{1, 0, 2}
	edges := []Edge{
		{Importer: 0, Imported: 1},
		{Importer: 1, Imported: 0},
		{Importer: 2, Imported: 0},
	}
	if got := StableOrder(current, edges); !slices.Equal(got, current) {
		t.Fatalf("cycle members did not retain current order: %#v", got)
	}
}

func TestStableOrderKeepsCycleMemberOrderWithExternalDependency(t *testing.T) {
	current := []int{1, 0, 2}
	edges := []Edge{
		{Importer: 0, Imported: 1},
		{Importer: 1, Imported: 0},
		{Importer: 1, Imported: 2},
	}
	if got := StableOrder(current, edges); !slices.Equal(got, []int{2, 1, 0}) {
		t.Fatalf("cycle member order changed around external dependency: %#v", got)
	}
}

func TestCohortDependenciesKeepsTopThreeEarlierInbound(t *testing.T) {
	cohorts := [][]int{{0, 1}, {10, 11}, {20}, {30}, {40, 41}}
	edges := []Edge{
		{Importer: 40, Imported: 0}, {Importer: 41, Imported: 1},
		{Importer: 40, Imported: 10}, {Importer: 41, Imported: 11},
		{Importer: 40, Imported: 20}, {Importer: 40, Imported: 30},
		{Importer: 10, Imported: 40},
		{Importer: 40, Imported: 41},
	}
	got := CohortDependencies(cohorts, edges)
	if !slices.Equal(got[4], []int{0, 1, 2}) {
		t.Fatalf("top inbound dependencies = %#v", got[4])
	}
	for index := 0; index < 4; index++ {
		if len(got[index]) != 0 {
			t.Fatalf("cohort %d dependencies = %#v", index, got[index])
		}
	}
}

func testFile(path string, lines ...string) document.File {
	hunkLines := make([]document.HunkLine, len(lines))
	for index, line := range lines {
		hunkLines[index] = document.HunkLine{Type: "a", Text: line}
	}
	return document.File{
		Path: path, Additions: len(lines),
		Hunks: []document.Hunk{{Lines: hunkLines}},
	}
}
