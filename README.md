# Python Gazelle plugin

[Gazelle](https://github.com/bazelbuild/bazel-gazelle)
is a build file generator for Bazel projects. It can create new BUILD.bazel files for a project that follows language conventions, and it can update existing build files to include new sources, dependencies, and options.

Gazelle may be run by Bazel using the gazelle rule, or it may be installed and run as a command line tool.

This directory contains a plugin for
[Gazelle](https://github.com/bazelbuild/bazel-gazelle)
that generates BUILD files content for Python code.

The following instructions are for when you use [bzlmod](https://docs.bazel.build/versions/5.0.0/bzlmod.html).
Please refer to older documentation that includes instructions on how to use Gazelle
without using bzlmod as your dependency manager.

## Example

We have an example of using Gazelle with Python located [here](https://github.com/bazelbuild/rules_python/tree/main/examples/build_file_generation).

## Adding Gazelle to your project

First, you'll need to add Gazelle to your `MODULES.bazel` file.
Get the current version of Gazelle from there releases here:  https://github.com/bazelbuild/bazel-gazelle/releases/.


See the installation `MODULE.bazel` snippet on the Releases page:
https://github.com/bazelbuild/rules_python/releases in order to configure rules_python.

You will also need to add the `bazel_dep` for configuration for `rules_python_gazelle_plugin`.

Here is a snippet of a `MODULE.bazel` file.

```starlark
# The following stanza defines the dependency rules_python.
bazel_dep(name = "rules_python", version = "0.20.0")

# The following stanza defines the dependency rules_python.
# For typical setups you set the version.
bazel_dep(name = "rules_python_gazelle_plugin", version = "0.20.0")

# The following stanza defines the dependency rules_python.
bazel_dep(name = "gazelle", version = "0.30.0", repo_name = "bazel_gazelle")
```
You will also need to do the other usual configuration for `rules_python` in your
`MODULE.bazel` file.

Next, we'll fetch metadata about your Python dependencies, so that gazelle can
determine which package a given import statement comes from. This is provided
by the `modules_mapping` rule. We'll make a target for consuming this
`modules_mapping`, and writing it as a manifest file for Gazelle to read.
This is checked into the repo for speed, as it takes some time to calculate
in a large monorepo.

Gazelle will walk up the filesystem from a Python file to find this metadata,
looking for a file called `gazelle_python.yaml` in an ancestor folder of the Python code.
Create an empty file with this name. It might be next to your `requirements.txt` file.
(You can just use `touch` at this point, it just needs to exist.)

To keep the metadata updated, put this in your `BUILD.bazel` file next to `gazelle_python.yaml`:

```starlark
load("@pip//:requirements.bzl", "all_whl_requirements")
load("@rules_python_gazelle_plugin//manifest:defs.bzl", "gazelle_python_manifest")
load("@rules_python_gazelle_plugin//modules_mapping:def.bzl", "modules_mapping")

# This rule fetches the metadata for python packages we depend on. That data is
# required for the gazelle_python_manifest rule to update our manifest file.
modules_mapping(
    name = "modules_map",
    wheels = all_whl_requirements,
)

# Gazelle python extension needs a manifest file mapping from
# an import to the installed package that provides it.
# This macro produces two targets:
# - //:gazelle_python_manifest.update can be used with `bazel run`
#   to recalculate the manifest
# - //:gazelle_python_manifest.test is a test target ensuring that
#   the manifest doesn't need to be updated
gazelle_python_manifest(
    name = "gazelle_python_manifest",
    modules_mapping = ":modules_map",
    # This is what we called our `pip_install` rule, where third-party
    # python libraries are loaded in BUILD files.
    pip_repository_name = "pip",
    # This should point to wherever we declare our python dependencies
    # (the same as what we passed to the modules_mapping rule in WORKSPACE)
    requirements = "//:requirements_lock.txt",
)
```

Finally, you create a target that you'll invoke to run the Gazelle tool
with the rules_python extension included. This typically goes in your root
`/BUILD.bazel` file:

```starlark
load("@bazel_gazelle//:def.bzl", "gazelle")
load("@rules_python_gazelle_plugin//:def.bzl", "GAZELLE_PYTHON_RUNTIME_DEPS")

# Our gazelle target points to the python gazelle binary.
# This is the simple case where we only need one language supported.
# If you also had proto, go, or other gazelle-supported languages,
# you would also need a gazelle_binary rule.
# See https://github.com/bazelbuild/bazel-gazelle/blob/master/extend.rst#example
gazelle(
    name = "gazelle",
    data = GAZELLE_PYTHON_RUNTIME_DEPS,
    gazelle = "@rules_python_gazelle_plugin//python:gazelle_binary",
)
```

That's it, now you can finally run `bazel run //:gazelle` anytime
you edit Python code, and it should update your `BUILD` files correctly.

A fully-working example is in [`examples/build_file_generation`](../examples/build_file_generation).

## Usage

Gazelle is non-destructive.
It will try to leave your edits to BUILD files alone, only making updates to `py_*` targets.
However it will remove dependencies that appear to be unused, so it's a
good idea to check in your work before running Gazelle so you can easily
revert any changes it made.

The rules_python extension assumes some conventions about your Python code.
These are noted below, and might require changes to your existing code.

Note that the `gazelle` program has multiple commands. At present, only the `update` command (the default) does anything for Python code.

### Directives

You can configure the extension using directives, just like for other
languages. These are just comments in the `BUILD.bazel` file which
govern behavior of the extension when processing files under that
folder.

See https://github.com/bazelbuild/bazel-gazelle#directives
for some general directives that may be useful.
In particular, the `resolve` directive is language-specific
and can be used with Python.
Examples of these directives in use can be found in the
/gazelle/testdata folder in the rules_python repo.

Python-specific directives are as follows:

| **Directive**                        | **Default value** |
|--------------------------------------|-------------------|
| `# gazelle:python_extension`         |   `enabled`       |
| Controls whether the Python extension is enabled or not. Sub-packages inherit this value. Can be either "enabled" or "disabled". | |
| `# gazelle:python_root`              |    n/a            |
| Sets a Bazel package as a Python root. This is used on monorepos with multiple Python projects that don't share the top-level of the workspace as the root. | |
| `# gazelle:python_manifest_file_name`| `gazelle_python.yaml` |
| Overrides the default manifest file name. | |
| `# gazelle:python_ignore_files`      |     n/a           |
| Controls the files which are ignored from the generated targets. | |
| `# gazelle:python_ignore_dependencies`|    n/a           |
| Controls the ignored dependencies from the generated targets. | |
| `# gazelle:python_validate_import_statements`| `true` |
| Controls whether the Python import statements should be validated. Can be "true" or "false" | |
| `# gazelle:python_generation_mode`| `package` |
| Controls the target generation mode. Can be "package" or "project" | |
| `# gazelle:python_library_naming_convention`| `$package_name$` |
| Controls the `py_library` naming convention. It interpolates $package_name$ with the Bazel package name. E.g. if the Bazel package name is `foo`, setting this to `$package_name$_my_lib` would result in a generated target named `foo_my_lib`. | |
| `# gazelle:python_binary_naming_convention` | `$package_name$_bin` |
| Controls the `py_binary` naming convention. Follows the same interpolation rules as `python_library_naming_convention`. | |
| `# gazelle:python_test_naming_convention` | `$package_name$_test` |
| Controls the `py_test` naming convention. Follows the same interpolation rules as `python_library_naming_convention`. | |
| `# gazelle:resolve py ...` | n/a |
| Instructs the plugin what target to add as a dependency to satisfy a given import statement. The syntax is `# gazelle:resolve py import-string label` where `import-string` is the symbol in the python `import` statement, and `label` is the Bazel label that Gazelle should write in `deps`. | |

### Libraries

Python source files are those ending in `.py` but not ending in `_test.py`.

First, we look for the nearest ancestor BUILD file starting from the folder
containing the Python source file.

If there is no `py_library` in this BUILD file, one is created, using the
package name as the target's name. This makes it the default target in the
package.

Next, all source files are collected into the `srcs` of the `py_library`.

Finally, the `import` statements in the source files are parsed, and
dependencies are added to the `deps` attribute.

### Unit Tests

A `py_test` target is added to the BUILD file when gazelle encounters
a file named `__test__.py`.
Often, Python unit test files are named with the suffix `_test`.
For example, if we had a folder that is a package named "foo" we could have a Python file named `foo_test.py` 
and gazelle would create a `py_test` block for the file.

The following is an example of a `py_test` target that gazelle would add when
it encounters a file named `__test__.py`.

```starlark
py_test(
    name = "build_file_generation_test",
    srcs = ["__test__.py"],
    main = "__test__.py",
    deps = [":build_file_generation"],
)
```

You can control the naming convention for test targets by adding a gazelle directive named
`# gazelle:python_test_naming_convention`.  See the instructions in the section above that 
covers directives.

### Binaries

When a `__main__.py` file is encountered, this indicates the entry point
of a Python program.

A `py_binary` target will be created, named `[package]_bin`.

## Developer Notes

Gazelle extensions are written in Go. This gazelle plugin is a hybrid, as it uses Go to execute a 
Python interpreter as a subprocess to parse Python source files. 
See the gazelle documentation https://github.com/bazelbuild/bazel-gazelle/blob/master/extend.md 
for more information on extending Gazelle.

If you add new Go dependencies to the plugin source code, you need to "tidy" the go.mod file. 
After changing that file, run `go mod tidy` or `bazel run @go_sdk//:bin/go -- mod tidy` 
to update the go.mod and go.sum files. Then run `bazel run //:update_go_deps` to have gazelle 
add the new dependenies to the deps.bzl file. The deps.bzl file is used as defined in our /WORKSPACE 
to include the external repos Bazel loads Go dependencies from.

Then after editing Go code, run `bazel run //:gazelle` to generate/update the rules in the 
BUILD.bazel files in our repo.
