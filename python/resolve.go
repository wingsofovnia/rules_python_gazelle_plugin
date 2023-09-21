// Copyright 2023 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package python

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
	"github.com/emirpasic/gods/sets/treeset"
	godsutils "github.com/emirpasic/gods/utils"

	"github.com/bazelbuild/rules_python/gazelle/pythonconfig"
)

const languageName = "py"

const (
	// resolvedDepsKey is the attribute key used to pass dependencies that don't
	// need to be resolved by the dependency resolver in the Resolver step.
	resolvedDepsKey = "_gazelle_python_resolved_deps"
)

// Resolver satisfies the resolve.Resolver interface. It resolves dependencies
// in rules generated by this extension.
type Resolver struct{}

// Name returns the name of the language. This is the prefix of the kinds of
// rules generated. E.g. py_library and py_binary.
func (*Resolver) Name() string { return languageName }

// Imports returns a list of ImportSpecs that can be used to import the rule
// r. This is used to populate RuleIndex.
//
// If nil is returned, the rule will not be indexed. If any non-nil slice is
// returned, including an empty slice, the rule will be indexed.
func (py *Resolver) Imports(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	cfgs := c.Exts[languageName].(pythonconfig.Configs)
	cfg := cfgs[f.Pkg]
	srcs := r.AttrStrings("srcs")
	provides := make([]resolve.ImportSpec, 0, len(srcs)+1)
	for _, src := range srcs {
		ext := filepath.Ext(src)
		if ext == ".py" {
			pythonProjectRoot := cfg.PythonProjectRoot()
			provide := importSpecFromSrc(pythonProjectRoot, f.Pkg, src)
			provides = append(provides, provide)
		}
	}
	if len(provides) == 0 {
		return nil
	}
	return provides
}

// importSpecFromSrc determines the ImportSpec based on the target that contains the src so that
// the target can be indexed for import statements that match the calculated src relative to the its
// Python project root.
func importSpecFromSrc(pythonProjectRoot, bzlPkg, src string) resolve.ImportSpec {
	pythonPkgDir := filepath.Join(bzlPkg, filepath.Dir(src))
	relPythonPkgDir, err := filepath.Rel(pythonProjectRoot, pythonPkgDir)
	if err != nil {
		panic(fmt.Errorf("unexpected failure: %v", err))
	}
	if relPythonPkgDir == "." {
		relPythonPkgDir = ""
	}
	pythonPkg := strings.ReplaceAll(relPythonPkgDir, "/", ".")
	filename := filepath.Base(src)
	if filename == pyLibraryEntrypointFilename {
		if pythonPkg != "" {
			return resolve.ImportSpec{
				Lang: languageName,
				Imp:  pythonPkg,
			}
		}
	}
	moduleName := strings.TrimSuffix(filename, ".py")
	var imp string
	if pythonPkg == "" {
		imp = moduleName
	} else {
		imp = fmt.Sprintf("%s.%s", pythonPkg, moduleName)
	}
	return resolve.ImportSpec{
		Lang: languageName,
		Imp:  imp,
	}
}

// Embeds returns a list of labels of rules that the given rule embeds. If
// a rule is embedded by another importable rule of the same language, only
// the embedding rule will be indexed. The embedding rule will inherit
// the imports of the embedded rule.
func (py *Resolver) Embeds(r *rule.Rule, from label.Label) []label.Label {
	// TODO(f0rmiga): implement.
	return make([]label.Label, 0)
}

// Resolve translates imported libraries for a given rule into Bazel
// dependencies. Information about imported libraries is returned for each
// rule generated by language.GenerateRules in
// language.GenerateResult.Imports. Resolve generates a "deps" attribute (or
// the appropriate language-specific equivalent) for each import according to
// language-specific rules and heuristics.
func (py *Resolver) Resolve(
	c *config.Config,
	ix *resolve.RuleIndex,
	rc *repo.RemoteCache,
	r *rule.Rule,
	modulesRaw interface{},
	from label.Label,
) {
	// TODO(f0rmiga): may need to be defensive here once this Gazelle extension
	// join with the main Gazelle binary with other rules. It may conflict with
	// other generators that generate py_* targets.
	deps := treeset.NewWith(godsutils.StringComparator)
	if modulesRaw != nil {
		cfgs := c.Exts[languageName].(pythonconfig.Configs)
		cfg := cfgs[from.Pkg]
		pythonProjectRoot := cfg.PythonProjectRoot()
		modules := modulesRaw.(*treeset.Set)
		it := modules.Iterator()
		explainDependency := os.Getenv("EXPLAIN_DEPENDENCY")
		hasFatalError := false
	MODULES_LOOP:
		for it.Next() {
			mod := it.Value().(module)
			moduleParts := strings.Split(mod.Name, ".")
			possibleModules := []string{mod.Name}
			for len(moduleParts) > 1 {
				// Iterate back through the possible imports until
				// a match is found.
				// For example, "from foo.bar import baz" where baz is a module, we should try `foo.bar.baz` first, then
				// `foo.bar`, then `foo`.
				// In the first case, the import could be file `baz.py` in the directory `foo/bar`.
				// Or, the import could be variable `baz` in file `foo/bar.py`.
				// The import could also be from a standard module, e.g. `six.moves`, where
				// the dependency is actually `six`.
				moduleParts = moduleParts[:len(moduleParts)-1]
				possibleModules = append(possibleModules, strings.Join(moduleParts, "."))
			}
			errs := []error{}
		POSSIBLE_MODULE_LOOP:
			for _, moduleName := range possibleModules {
				imp := resolve.ImportSpec{Lang: languageName, Imp: moduleName}
				if override, ok := resolve.FindRuleWithOverride(c, imp, languageName); ok {
					if override.Repo == "" {
						override.Repo = from.Repo
					}
					if !override.Equal(from) {
						if override.Repo == from.Repo {
							override.Repo = ""
						}
						dep := override.String()
						deps.Add(dep)
						if explainDependency == dep {
							log.Printf("Explaining dependency (%s): "+
								"in the target %q, the file %q imports %q at line %d, "+
								"which resolves using the \"gazelle:resolve\" directive.\n",
								explainDependency, from.String(), mod.Filepath, moduleName, mod.LineNumber)
						}
						continue MODULES_LOOP
					}
				} else {
					if dep, ok := cfg.FindThirdPartyDependency(moduleName); ok {
						deps.Add(dep)
						if explainDependency == dep {
							log.Printf("Explaining dependency (%s): "+
								"in the target %q, the file %q imports %q at line %d, "+
								"which resolves from the third-party module %q from the wheel %q.\n",
								explainDependency, from.String(), mod.Filepath, moduleName, mod.LineNumber, mod.Name, dep)
						}
						continue MODULES_LOOP
					} else {
						matches := ix.FindRulesByImportWithConfig(c, imp, languageName)
						if len(matches) == 0 {
							// Check if the imported module is part of the standard library.
							if isStd, err := isStdModule(module{Name: moduleName}); err != nil {
								log.Println("Error checking if standard module: ", err)
								hasFatalError = true
								continue POSSIBLE_MODULE_LOOP
							} else if isStd {
								continue MODULES_LOOP
							} else if cfg.ValidateImportStatements() {
								err := fmt.Errorf(
									"%[1]q at line %[2]d from %[3]q is an invalid dependency: possible solutions:\n"+
										"\t1. Add it as a dependency in the requirements.txt file.\n"+
										"\t2. Instruct Gazelle to resolve to a known dependency using the gazelle:resolve directive.\n"+
										"\t3. Ignore it with a comment '# gazelle:ignore %[1]s' in the Python file.\n",
									moduleName, mod.LineNumber, mod.Filepath,
								)
								errs = append(errs, err)
								continue POSSIBLE_MODULE_LOOP
							}
						}
						filteredMatches := make([]resolve.FindResult, 0, len(matches))
						for _, match := range matches {
							if match.IsSelfImport(from) {
								// Prevent from adding itself as a dependency.
								continue MODULES_LOOP
							}
							filteredMatches = append(filteredMatches, match)
						}
						if len(filteredMatches) == 0 {
							continue POSSIBLE_MODULE_LOOP
						}
						if len(filteredMatches) > 1 {
							sameRootMatches := make([]resolve.FindResult, 0, len(filteredMatches))
							for _, match := range filteredMatches {
								if strings.HasPrefix(match.Label.Pkg, pythonProjectRoot) {
									sameRootMatches = append(sameRootMatches, match)
								}
							}
							if len(sameRootMatches) != 1 {
								err := fmt.Errorf(
									"multiple targets (%s) may be imported with %q at line %d in %q "+
										"- this must be fixed using the \"gazelle:resolve\" directive",
									targetListFromResults(filteredMatches), moduleName, mod.LineNumber, mod.Filepath)
								errs = append(errs, err)
								continue POSSIBLE_MODULE_LOOP
							}
							filteredMatches = sameRootMatches
						}
						matchLabel := filteredMatches[0].Label.Rel(from.Repo, from.Pkg)
						dep := matchLabel.String()
						deps.Add(dep)
						if explainDependency == dep {
							log.Printf("Explaining dependency (%s): "+
								"in the target %q, the file %q imports %q at line %d, "+
								"which resolves from the first-party indexed labels.\n",
								explainDependency, from.String(), mod.Filepath, moduleName, mod.LineNumber)
						}
						continue MODULES_LOOP
					}
				}
			} // End possible modules loop.
			if len(errs) > 0 {
				// If, after trying all possible modules, we still haven't found anything, error out.
				joinedErrs := ""
				for _, err := range errs {
					joinedErrs = fmt.Sprintf("%s%s\n", joinedErrs, err)
				}
				log.Printf("ERROR: failed to validate dependencies for target %q: %v\n", from.String(), joinedErrs)
				hasFatalError = true
			}
		}
		if hasFatalError {
			os.Exit(1)
		}
	}
	resolvedDeps := r.PrivateAttr(resolvedDepsKey).(*treeset.Set)
	if !resolvedDeps.Empty() {
		it := resolvedDeps.Iterator()
		for it.Next() {
			deps.Add(it.Value())
		}
	}
	if !deps.Empty() {
		r.SetAttr("deps", convertDependencySetToExpr(deps))
	}
}

// targetListFromResults returns a string with the human-readable list of
// targets contained in the given results.
func targetListFromResults(results []resolve.FindResult) string {
	list := make([]string, len(results))
	for i, result := range results {
		list[i] = result.Label.String()
	}
	return strings.Join(list, ", ")
}

// convertDependencySetToExpr converts the given set of dependencies to an
// expression to be used in the deps attribute.
func convertDependencySetToExpr(set *treeset.Set) bzl.Expr {
	deps := make([]bzl.Expr, set.Size())
	it := set.Iterator()
	for it.Next() {
		dep := it.Value().(string)
		deps[it.Index()] = &bzl.StringExpr{Value: dep}
	}
	return &bzl.ListExpr{List: deps}
}
