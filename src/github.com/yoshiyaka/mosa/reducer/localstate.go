package reducer

import (
	"fmt"

	. "github.com/yoshiyaka/mosa/manifest"
)

// Holds local state when resolving a node, class or define. This includes stuff
// such as which variables are currently defined, and what values they hold.
type localState struct {
	// Contains a map of all top level variable definitions seen in the class.
	varDefsByName map[string]VariableDef

	// When a variable is resolved, it will be removed from varDefsByName and
	// stored here with its final value.
	resolvedVars map[string]Value

	// These helps us return nice error messages. They hold information of where
	// this class/node/define was realized.
	definedInFile  string
	realizedInFile string
	realizedAtLine int
}

func newLocalState(definedInFile, realizedInFile string, realizedAtLine int) *localState {
	return &localState{
		varDefsByName:  map[string]VariableDef{},
		resolvedVars:   map[string]Value{},
		definedInFile:  definedInFile,
		realizedInFile: realizedInFile,
		realizedAtLine: realizedAtLine,
	}
}

func (ls *localState) resolveVariable(v VariableName, lineNum int) (Value, error) {
	return ls.resolveVariableRecursive(
		v, lineNum, nil, map[VariableName]bool{},
	)
}

// Recursively resolves a variable's actual value.
//
// chain will keep the chain used to define the variable, for instance if
// a manifest looks like
//  $foo = $bar
//  $bar = 3
// chain will contain [ $foo, $bar ]. This is used when printing errors about
// cyclic dependencies.
//
// seenNames is keeps track of all variables already seen during the current
// recursion. Used to detect cyclic dependencies.
func (ls *localState) resolveVariableRecursive(lookingFor VariableName, lineNum int, chain []*VariableDef, seenNames map[VariableName]bool) (Value, error) {
	if val, found := ls.resolvedVars[lookingFor.Str]; found {
		return val, nil
	}

	foundVar, found := ls.varDefsByName[lookingFor.Str]
	if !found {
		return nil, &Err{
			Line:       lineNum,
			Type:       ErrorTypeUnresolvableVariable,
			SymbolName: string(lookingFor.String()),
		}
	}

	if _, isVar := foundVar.Val.(VariableName); !isVar {
		// This is an actual value
		if array, isArray := foundVar.Val.(Array); isArray {
			// The value pointed to is an array. Resolve all values in the array
			// aswell.
			seenNamesCopy := map[VariableName]bool{}
			for key, val := range seenNames {
				seenNamesCopy[key] = val
			}

			resolvedArray, err := ls.resolveArrayRecursive(
				array, lineNum, chain, seenNamesCopy,
			)
			if err == nil {
				ls.resolvedVars[lookingFor.Str] = resolvedArray
				delete(ls.varDefsByName, lookingFor.Str)
			}
			return resolvedArray, err
		} else {
			ls.resolvedVars[lookingFor.Str] = foundVar.Val
			delete(ls.varDefsByName, lookingFor.Str)

			return foundVar.Val, nil
		}
	}

	if _, seen := seenNames[lookingFor]; seen {
		cycle := make([]string, len(chain)+1)
		for i, def := range chain {
			cycle[i] = string(def.VariableName.Str)
		}
		cycle[len(cycle)-1] = string(lookingFor.Str)

		return nil, &CyclicError{
			Err: Err{
				Line:       chain[0].LineNum,
				Type:       ErrorTypeCyclicVariable,
				SymbolName: string(chain[0].VariableName.Str),
			},
			Cycle: cycle,
		}
	}

	seenNames[lookingFor] = true

	return ls.resolveVariableRecursive(
		foundVar.Val.(VariableName), foundVar.LineNum, append(chain, &foundVar),
		seenNames,
	)
}

func (ls *localState) resolveArrayRecursive(a Array, lineNum int, chain []*VariableDef, seenNames map[VariableName]bool) (Array, error) {
	newArray := make(Array, len(a))

	for i, val := range a {
		if varName, isVar := val.(VariableName); isVar {
			// This array entry is a variable name, resolve it.
			var err error
			seenNamesCopy := map[VariableName]bool{}
			for key, val := range seenNames {
				seenNamesCopy[key] = val
			}

			newArray[i], err = ls.resolveVariableRecursive(
				varName, lineNum, chain, seenNamesCopy,
			)
			if err != nil {
				return nil, err
			}
		} else {
			var err error
			newArray[i], err = ls.resolveValue(val, lineNum)
			if err != nil {
				return nil, err
			}
		}
	}

	return newArray, nil
}

func (ls *localState) resolveValue(v Value, lineNum int) (Value, error) {
	switch v.(type) {
	case VariableName:
		return ls.resolveVariable(v.(VariableName), lineNum)
	case Array:
		return ls.resolveArray(v.(Array), lineNum)
	case Reference:
		return ls.resolveReference(v.(Reference))
	default:
		return v, nil
	}
}

// Defines local variables from an array of arguments. This is used when a class
// or define is being realized with a set of custom arguments passed to it.
func (ls *localState) setVarsFromArgs(passedArgs []Prop, availableParams []VariableDef) error {
	argsByName := map[string]*Prop{}
	for i, arg := range passedArgs {
		argsByName[arg.Name] = &passedArgs[i]
	}

	// Ignore depends => ...
	delete(argsByName, "depends")

	for _, def := range availableParams {
		if _, exists := ls.varDefsByName[def.VariableName.Str]; exists {
			return &Err{
				Line:       def.LineNum,
				Type:       ErrorTypeMultipleDefinition,
				SymbolName: string(def.VariableName.Str),
			}
		}

		if arg, hasArg := argsByName[def.VariableName.Str[1:]]; hasArg {
			// Pass the argument value
			def.Val = arg.Value
			delete(argsByName, arg.Name)
		}

		if def.Val == nil {
			return fmt.Errorf(
				"Required argument '%s' not supplied at %s:%d",
				def.VariableName.Str[1:], ls.realizedInFile, ls.realizedAtLine,
			)
		}

		ls.varDefsByName[def.VariableName.Str] = def
	}

	// Make sure no args which doesn't exist in the class was passed to it.
	if len(argsByName) > 0 {
		for _, arg := range argsByName {
			return fmt.Errorf(
				"Unsupported argument '%s' sent to class at %s:%d",
				arg.Name, ls.realizedInFile, arg.LineNum,
			)
		}
	}

	return nil
}

func (ls *localState) resolveArray(a Array, lineNum int) (Array, error) {
	return ls.resolveArrayRecursive(
		a, lineNum, nil, map[VariableName]bool{},
	)
}

func (ls *localState) resolveReference(r Reference) (Reference, error) {
	switch r.Scalar.(type) {
	case QuotedString:
		return r, nil
	case VariableName:
		var err error
		varName := r.Scalar.(VariableName)
		r.Scalar, err = ls.resolveVariable(varName, r.LineNum)
		if err != nil {
			return r, err
		} else if _, isString := r.Scalar.(QuotedString); !isString {
			return r, fmt.Errorf(
				"Reference keys must be strings at %s:%d - the value of %s is not.",
				ls.definedInFile, r.LineNum, varName.Str,
			)
		} else {
			return r, nil
		}

	default:
		return r, fmt.Errorf(
			"Reference keys must be strings at %s:%d",
			ls.definedInFile, r.LineNum,
		)
	}
}

// Resolves the values in a property list into concrete values.
func (ls *localState) resolveProps(props []Prop) ([]Prop, error) {
	ret := make([]Prop, len(props))

	for i, prop := range props {
		if varName, pointsToVar := prop.Value.(VariableName); pointsToVar {
			var err error
			prop.Value, err = ls.resolveVariable(varName, prop.LineNum)
			if err != nil {
				return nil, err
			}
			ret[i] = prop
		} else {
			var err error
			prop.Value, err = ls.resolveValue(prop.Value, prop.LineNum)
			if err != nil {
				return nil, err
			}
			ret[i] = prop
		}
	}

	return ret, nil
}