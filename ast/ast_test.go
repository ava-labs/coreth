package ast

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func getPs() ([]*packages.Package, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	parent := filepath.Dir(dir)
	fmt.Println(parent)

	return packages.Load(
		&packages.Config{
			Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
		},
		fmt.Sprintf("%s/...", parent),
	)
}

func mkFile(pkg, file string) entity {
	pkg = strings.ReplaceAll(pkg, "/", ".")
	return entity{
		id:      fmt.Sprintf("%s.%s", pkg, file),
		display: file,
		kind:    "file",
	}
}

func mkType(pkg, typ string, kind string) entity {
	switch kind {
	case "signature":
		return entity{
			id:      fmt.Sprintf("%s.%s", strings.ReplaceAll(pkg, "/", "."), typ),
			display: typ,
			kind:    "signature",
		}
	case "named:interface":
		kind = "interface"
	default:
		kind = "struct"
	}

	return entity{
		id:      fmt.Sprintf("%s/%s", pkg, typ),
		display: typ,
		kind:    kind,
	}
}

type entity struct {
	id      string
	display string
	kind    string
}

type uses struct {
	namespaces map[string]map[string]entity
	uses       map[string]map[string]struct{}
}

func newUses() *uses {
	return &uses{
		namespaces: make(map[string]map[string]entity),
		uses:       make(map[string]map[string]struct{}),
	}
}

func (u *uses) Visit(usePkg, useFile, defPkg, defType string, kind string) {
	bits := strings.Split(useFile, "/")
	fn := bits[len(bits)-1]
	useFile = fn

	skipDef := inSpec(packagesToSkip, defPkg, defType)
	if skipDef {
		return
	}

	expandDef := inSpec(packagesToExpand, defPkg, defType)
	// if !expandDef {
	// 	defType = "package"
	// 	kind = "package"
	// }
	expandUse := inSpec(packagesToExpand, usePkg, useFile)
	// if !expandUse {
	// 	useFile = "package"
	// }
	if !expandDef && !expandUse {
		return
	}

	if _, ok := u.namespaces[usePkg]; !ok {
		u.namespaces[usePkg] = make(map[string]entity)
	}
	fileEntity := mkFile(usePkg, useFile)
	u.namespaces[usePkg][useFile] = fileEntity
	if _, ok := u.namespaces[defPkg]; !ok {
		u.namespaces[defPkg] = make(map[string]entity)
	}
	typeEntity := mkType(defPkg, defType, kind)
	u.namespaces[defPkg][defType] = typeEntity

	if _, ok := u.uses[typeEntity.id]; !ok {
		u.uses[typeEntity.id] = make(map[string]struct{})
	}
	u.uses[typeEntity.id][fileEntity.id] = struct{}{}
}

func (u *uses) Write() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	//fn := "USES.md"
	fn := "USES.plantuml"
	f, err := os.Create(filepath.Join(wd, fn))
	if err != nil {
		return err
	}
	defer f.Close()
	//f.WriteString("```mermaid\n")
	//f.WriteString("flowchart LR\n")
	f.WriteString("@startuml\n")
	f.WriteString(`
		left to right direction
		set namespaceSeparator /
		allow_mixing
		scale 750 width
		skinparam nodesep 10
		skinparam ranksep 400
	`,
	)

	// write out
	for pkg, ns := range u.namespaces {
		f.WriteString(fmt.Sprintf("package %s {\n", pkg))
		for _, entity := range ns {
			var outStr string
			if entity.kind == "file" {
				outStr = fmt.Sprintf(" note \"%s\" as %s\n", entity.display, entity.id)
			} else if entity.kind == "signature" {
				outStr = fmt.Sprintf(" card \"%s\" as %s\n", entity.display, entity.id)
			} else if entity.kind == "interface" {
				outStr = fmt.Sprintf(" interface %s\n", entity.id)
			} else {
				outStr = fmt.Sprintf(" struct \"%s\" as %s\n", entity.display, entity.id)
			}
			f.WriteString(outStr)
		}
		f.WriteString("}\n")
	}

	for def, uses := range u.uses {
		for use := range uses {
			f.WriteString(fmt.Sprintf(" \"%s\" --> \"%s\"\n", use, def))
		}
	}

	return nil
}

type specMap map[string]map[string]struct{}

var (
	packagesToSkip = specMap{
		"goethereum/common":          {},
		"goethereum/common/math":     {},
		"goethereum/rlp":             {},
		"goethereum/crypto":          {},
		"goethereum/crypto/bn256":    {},
		"goethereum/crypto/blake2b":  {},
		"goethereum/crypto/bls12381": {},
		"goethereum/log":             {},
		"coreth/core/vm":             {"OpCode": {}, "Config": {}},
		"coreth/utils":               {},
	}
	packagesToTrack = map[string]string{
		"github.com/ethereum/go-ethereum": "goethereum",
		"github.com/ava-labs/coreth":      "coreth",
	}
	packagesToExpand = specMap{
		"coreth/core/vm": {},
		"coreth/core": {"evm.go": {}, "state_prefetcher.go": {},
			"state_processor.go": {}, "state_transition.go": {},
		},
		//	"coreth/geth/core/vm": {},
	}
)

func inSpec(specMap specMap, pkg, typ string) bool {
	named, ok := specMap[pkg]
	if !ok {
		return false
	}
	if len(named) == 0 {
		return true
	}
	_, ok = named[typ]
	return ok
}

func removePrefix(id string) (string, bool) {
	for prefix, replace := range packagesToTrack {
		if strings.HasPrefix(id, prefix) {
			return replace + id[len(prefix):], true
		}
	}
	return "", false
}

func visitDecl(pkg string, file string, id *ast.Ident) {
	if !id.IsExported() {
		return
	}

	bits := strings.Split(file, "/")
	fn := bits[len(bits)-1]
	file = fn

	if _, ok := packagesToExpand[pkg]; !ok {
		return
	}
	if _, ok := packagesToExpand[pkg][file]; !ok {
		return
	}
	packagesToExpand[pkg][id.Name] = struct{}{}
	fmt.Printf("expanding: %v:%v (%v)\n", pkg, id.Name, file)
}

func TestY(t *testing.T) {
	require := require.New(t)
	ps, err := getPs()
	require.NoError(err)

	u := newUses()
	for _, p := range ps {
		pkgName := p.ID
		defPkg, found := removePrefix(pkgName)
		if !found {
			continue
		}
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				name := p.Fset.File(f.FileStart).Name()
				switch decl := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							visitDecl(defPkg, name, spec.Name)
						case *ast.ValueSpec:
							for _, id := range spec.Names {
								visitDecl(defPkg, name, id)
							}
						}
					}
				case *ast.FuncDecl:
					visitDecl(defPkg, name, decl.Name)
				}
			}
		}
	}
	fmt.Println(packagesToExpand)

	for i, p := range ps {
		usePkg, _ := removePrefix(p.ID)
		fmt.Printf("%d: %v\n", i, p.ID)
		for _, f := range p.Syntax {
			name := p.Fset.File(f.FileStart).Name()
			fmt.Printf("  %v\n", name)
			ast.Inspect(f, func(n ast.Node) bool {
				if n, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := n.X.(*ast.Ident); ok {
						use := p.TypesInfo.Uses[id]
						if pkgName, ok := use.(*types.PkgName); ok {
							defPkg, found := removePrefix(pkgName.Imported().Path())
							if !found {
								return true
							}

							use2 := p.TypesInfo.Types[n]
							kind := kind(use2.Type)
							if use2.IsValue() && kind != "signature" {
								return true
							}
							fmt.Printf(
								" 	%v, %v %v %v %v %v %v\n",
								n.X, n.Sel, defPkg, kind, use2.IsValue(),
								use2.Assignable(), use2.Addressable(),
							)
							u.Visit(usePkg, name, defPkg, n.Sel.Name, kind)
						}
					}
				}
				return true
			})
		}
	}
	require.NoError(u.Write())
}

func kind(t types.Type) string {
	switch t := t.(type) {
	case *types.Interface:
		return "interface"
	case *types.Struct:
		return "struct"
	case *types.Named:
		return "named:" + kind(t.Underlying())
	case *types.Signature:
		return "signature"
	case *types.Pointer:
		return "*" + kind(t.Elem())
	case *types.Basic:
		return "basic"
	case *types.Slice:
		return "[]" + kind(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), kind(t.Elem()))
	case *types.Map:
		return fmt.Sprintf("map[%s]%s", kind(t.Key()), kind(t.Elem()))
	case *types.TypeParam:
		return "typeparam:" + kind(t.Underlying())
	default:
		panic(fmt.Sprintf("unknown type %T", t))
	}
}

func TestX(t *testing.T) {
	require := require.New(t)
	ps, err := getPs()
	require.NoError(err)
	for i, p := range ps {
		fmt.Printf("%v %v\n", p.Name, p.Types)
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				if n, ok := n.(*ast.Field); ok {
					fmt.Printf("-- %T, %v\n", n.Type, n.Type)
					err := visitType(p, n.Type)
					require.NoError(err)
				}
				return true
			})
		}
		if i > 1 {
			break
		}
	}
}

func visitType(p *packages.Package, n ast.Node) error {
	if n, ok := n.(*ast.SelectorExpr); ok {
		if id, ok := n.X.(*ast.Ident); ok {
			if _, ok := p.TypesInfo.Uses[id]; ok {
				fmt.Printf("OK * %v %v\n", id, n.Sel.Name)
			}
			if id.Obj != nil {
				fmt.Printf("** %v %v %v\n", id, id.Obj.Kind, n.Sel.Name)
			} else {
				fmt.Printf("x* %v %v\n", id, n.Sel.Name)
			}
		}
	}
	return nil
}
