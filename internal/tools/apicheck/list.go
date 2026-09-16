package main

import (
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// listModule loads the public packages of the module in dir and returns its
// API lines, sorted, and the module path.
func listModule(dir string) ([]string, string, error) {
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, "", err
	}
	modulePath := modulePathOf(gomod)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedModule,
		Dir:  dir,
		// Build tags and platforms don't change gorbital's API; list it as
		// Linux builds it.
		Env: append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, "", err
	}
	var errs []error
	lines := map[string]bool{}
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			errs = append(errs, fmt.Errorf("%s: %v", pkg.PkgPath, e))
		}
		if pkg.Module == nil || pkg.Module.Path != modulePath || pkg.Name == "main" || isInternal(pkg.PkgPath) || pkg.Types == nil {
			continue
		}
		for _, l := range listPackage(pkg.Types) {
			lines[l] = true
		}
	}
	if len(errs) > 0 {
		return nil, "", errors.Join(errs...)
	}
	return slices.Sorted(func(yield func(string) bool) {
		for l := range lines {
			if !yield(l) {
				return
			}
		}
	}), modulePath, nil
}

func isInternal(path string) bool {
	return slices.Contains(strings.Split(path, "/"), "internal")
}

// listPackage returns the API lines of one package.
func listPackage(pkg *types.Package) []string {
	prefix := "pkg " + pkg.Path() + ", "
	qualifier := func(other *types.Package) string {
		if other == pkg {
			return ""
		}
		return other.Name()
	}
	typ := func(t types.Type) string { return types.TypeString(withoutNames(t), qualifier) }

	var lines []string
	add := func(format string, args ...any) { lines = append(lines, prefix+fmt.Sprintf(format, args...)) }

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *types.Const:
			add("const %s %s", name, constType(obj.Type(), typ))
			add("const %s = %s", name, obj.Val().ExactString())
		case *types.Var:
			add("var %s %s", name, typ(obj.Type()))
		case *types.Func:
			sig := obj.Type().(*types.Signature)
			add("func %s%s%s", name, typeParams(sig.TypeParams(), typ), signature(sig, typ))
		case *types.TypeName:
			listType(obj, typ, add)
		}
	}
	return lines
}

// listType adds a type's declaration, its exported fields or interface
// methods, and the exported methods of its method set.
func listType(obj *types.TypeName, typ func(types.Type) string, add func(string, ...any)) {
	name := obj.Name()
	if obj.IsAlias() {
		add("type %s = %s", name, typ(types.Unalias(obj.Type())))
		return
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return
	}
	decl := name + typeParams(named.TypeParams(), typ)
	switch u := named.Underlying().(type) {
	case *types.Struct:
		add("type %s struct", decl)
		for i := range u.NumFields() {
			f := u.Field(i)
			switch {
			case f.Embedded():
				add("type %s struct, embedded %s", decl, typ(f.Type()))
			case f.Exported():
				add("type %s struct, %s %s", decl, f.Name(), typ(f.Type()))
			}
		}
	case *types.Interface:
		var methods []string
		for i := range u.NumMethods() {
			m := u.Method(i)
			methods = append(methods, m.Name())
			if m.Exported() {
				add("type %s interface, %s%s", decl, m.Name(), signature(m.Type().(*types.Signature), typ))
			}
		}
		for i := range u.NumEmbeddeds() {
			if _, isNamed := u.EmbeddedType(i).(*types.Named); !isNamed {
				methods = append(methods, typ(u.EmbeddedType(i))) // a type constraint union
			}
		}
		slices.Sort(methods)
		add("type %s interface { %s }", decl, strings.Join(methods, ", "))
	default:
		add("type %s %s", decl, typ(u))
	}

	// Methods, including promoted ones: value receivers on T, the rest on *T.
	value := types.NewMethodSet(named)
	pointer := types.NewMethodSet(types.NewPointer(named))
	if _, isInterface := named.Underlying().(*types.Interface); isInterface {
		return // listed above
	}
	for i := range pointer.Len() {
		m := pointer.At(i).Obj().(*types.Func)
		if !m.Exported() {
			continue
		}
		recv := "*" + name
		if value.Lookup(m.Pkg(), m.Name()) != nil {
			recv = name
		}
		add("method (%s) %s%s", recv, m.Name(), signature(m.Type().(*types.Signature), typ))
	}
}

// constType names an untyped constant's kind like Go's api files do.
func constType(t types.Type, typ func(types.Type) string) string {
	if b, ok := t.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return strings.Replace(b.Name(), "untyped ", "ideal-", 1)
	}
	return typ(t)
}

func typeParams(list *types.TypeParamList, typ func(types.Type) string) string {
	if list.Len() == 0 {
		return ""
	}
	params := make([]string, list.Len())
	for i := range list.Len() {
		p := list.At(i)
		params[i] = p.Obj().Name() + " " + typ(p.Constraint())
	}
	return "[" + strings.Join(params, ", ") + "]"
}

// signature formats parameters and results by type only, as go1.txt does.
func signature(sig *types.Signature, typ func(types.Type) string) string {
	tuple := func(t *types.Tuple, variadic bool) []string {
		out := make([]string, t.Len())
		for i := range t.Len() {
			if variadic && i == t.Len()-1 {
				out[i] = "..." + typ(t.At(i).Type().(*types.Slice).Elem())
				continue
			}
			out[i] = typ(t.At(i).Type())
		}
		return out
	}
	s := "(" + strings.Join(tuple(sig.Params(), sig.Variadic()), ", ") + ")"
	switch results := tuple(sig.Results(), false); len(results) {
	case 0:
	case 1:
		s += " " + results[0]
	default:
		s += " (" + strings.Join(results, ", ") + ")"
	}
	return s
}

// withoutNames drops parameter names from function types inside t, so
// renaming a parameter doesn't change the listing.
func withoutNames(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.Signature:
		tuple := func(in *types.Tuple) *types.Tuple {
			vars := make([]*types.Var, in.Len())
			for i := range in.Len() {
				v := in.At(i)
				vars[i] = types.NewParam(v.Pos(), v.Pkg(), "", withoutNames(v.Type()))
			}
			return types.NewTuple(vars...)
		}
		return types.NewSignatureType(nil, nil, nil, tuple(t.Params()), tuple(t.Results()), t.Variadic())
	case *types.Pointer:
		return types.NewPointer(withoutNames(t.Elem()))
	case *types.Slice:
		return types.NewSlice(withoutNames(t.Elem()))
	case *types.Array:
		return types.NewArray(withoutNames(t.Elem()), t.Len())
	case *types.Map:
		return types.NewMap(withoutNames(t.Key()), withoutNames(t.Elem()))
	case *types.Chan:
		return types.NewChan(t.Dir(), withoutNames(t.Elem()))
	}
	return t
}
