/*
 *
 * Copyright 2024 tofuutils authors.
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
 *
 */

package tfparser

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

const requiredVersionName = "required_version"

type extDescription struct {
	value    string
	len      int
	parseHCL bool
}

var exts = []extDescription{{value: ".tf", parseHCL: true}, {value: ".tf.json", parseHCL: false}} //nolint

var terraformPartialSchema = &hcl.BodySchema{ //nolint
	Blocks: []hcl.BlockHeaderSchema{{Type: "terraform"}},
}

var versionPartialSchema = &hcl.BodySchema{ //nolint
	Attributes: []hcl.AttributeSchema{{Name: requiredVersionName}},
}

func init() {
	for i, desc := range exts {
		desc.len = len(desc.value)
		exts[i] = desc // override with updated copy
	}
}

func GatherRequiredVersion(verbose bool) ([]string, error) {
	if verbose {
		fmt.Println("Scan project to find .tf files") //nolint
	}

	var requireds []string
	var foundFiles []string
	parser := hclparse.NewParser()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		pathLen := len(path)
		var parsedFile *hcl.File
		var diags hcl.Diagnostics
		for _, extDesc := range exts {
			if start := pathLen - extDesc.len; start < 0 || path[start:] != extDesc.value {
				continue
			}

			foundFiles = append(foundFiles, path)

			if extDesc.parseHCL {
				parsedFile, diags = parser.ParseHCLFile(path)
			} else {
				parsedFile, diags = parser.ParseJSONFile(path)
			}
			if diags.HasErrors() {
				return diags
			}
			if parsedFile == nil {
				return nil
			}

			extracted := extractRequiredVersion(parsedFile.Body, verbose)
			requireds = append(requireds, extracted...)

			return nil
		}

		return nil
	})

	if verbose {
		if len(foundFiles) == 0 {
			fmt.Println("No .tf file found") //nolint
		} else {
			fmt.Println("Read :", foundFiles) //nolint
		}
	}

	return requireds, err
}

func extractRequiredVersion(body hcl.Body, verbose bool) []string {
	rootContent, _, diags := body.PartialContent(terraformPartialSchema)
	if diags.HasErrors() {
		if verbose {
			fmt.Println("Failed to parse tf file :", diags) //nolint
		}

		return nil
	}

	requireds := make([]string, 0, 1)
	for _, block := range rootContent.Blocks {
		content, _, diags := block.Body.PartialContent(versionPartialSchema)
		if diags.HasErrors() {
			if verbose {
				fmt.Println("Failed to parse tf block :", diags) //nolint
			}

			return nil
		}

		attr, exists := content.Attributes[requiredVersionName]
		if !exists {
			continue
		}

		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			if verbose {
				fmt.Println("Failures parsing tf attribute :", diags) //nolint
			}

			return nil
		}

		val, err := convert.Convert(val, cty.String)
		if err != nil {
			if verbose {
				fmt.Println("Failed to convert tf attribute :", err) //nolint
			}

			return nil
		}

		if val.IsNull() {
			if verbose {
				fmt.Println("Empty tf attribute") //nolint
			}

			continue
		}

		if !val.IsWhollyKnown() {
			if verbose {
				fmt.Println("Unknown tf attribute") //nolint
			}

			continue
		}
		requireds = append(requireds, val.AsString())
	}

	return requireds
}
