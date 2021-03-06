package resolver

import (
	"fmt"

	. "github.com/yoshiyaka/mosa/ast"
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

	chain = append(chain, &foundVar)
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

	if _, isVar := foundVar.Val.(VariableName); !isVar {
		// This is an actual value, not a variable name. Recurse and continue
		// resolve in case the value is some thing that needs to be resovled,
		// such as an array or an interpolated string.
		resolved, err := ls.resolveValueRecursive(
			foundVar.Val, foundVar.LineNum, chain, seenNames,
		)

		if err == nil {
			ls.resolvedVars[lookingFor.Str] = resolved
			delete(ls.varDefsByName, lookingFor.Str)
		}

		return resolved, err
	}

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

func (ls *localState) resolveInterpolatedStringRecursive(is InterpolatedString, chain []*VariableDef, seenNames map[VariableName]bool) (QuotedString, error) {
	ret := ""

	for _, part := range is.Segments {
		if v, isVar := part.(VariableName); isVar {
			// This segment is a variable name, resolve it.
			seenNamesCopy := map[VariableName]bool{}
			for key, val := range seenNames {
				seenNamesCopy[key] = val
			}

			if val, err := ls.resolveVariableRecursive(
				v, is.LineNum, chain, seenNamesCopy,
			); err != nil {
				return "", err
			} else {
				switch val.(type) {
				case string:
					ret += val.(string)
				case QuotedString:
					ret += string(val.(QuotedString))
				default:
					fmt.Println(val)
					panic("value can't be interpolated")
				}
			}
		} else {
			ret += part.(string)
		}
	}

	return QuotedString(ret), nil
}

func (ls *localState) resolveValue(v Value, lineNum int) (Value, error) {
	return ls.resolveValueRecursive(v, lineNum, nil, map[VariableName]bool{})
}

func (ls *localState) resolveValueRecursive(v Value, lineNum int, chain []*VariableDef, seenNames map[VariableName]bool) (Value, error) {
	switch v.(type) {
	case VariableName:
		return ls.resolveVariableRecursive(
			v.(VariableName), lineNum, chain, seenNames,
		)
	case Array:
		return ls.resolveArrayRecursive(v.(Array), lineNum, chain, seenNames)
	case Reference:
		return ls.resolveReferenceRecursive(v.(Reference), chain, seenNames)
	case InterpolatedString:
		return ls.resolveInterpolatedStringRecursive(
			v.(InterpolatedString), chain, seenNames,
		)
	case Expression:
		return ls.resolveExpression(v.(Expression))
	default:
		return v, nil
	}
}

func (ls *localState) resolveExpression(e Expression) (v Value, retErr error) {
	left, leftErr := ls.resolveValue(e.Left, e.LineNum)
	if leftErr != nil {
		return nil, leftErr
	}
	right, rightErr := ls.resolveValue(e.Right, e.LineNum)
	if rightErr != nil {
		return nil, rightErr
	}

	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf(
				"Bad types (%T, %T) supplied for operation '%s' at %s:%d",
				left, right, e.Operation, ls.definedInFile, e.LineNum,
			)
		}
	}()

	// The functions below will panic if one of the types are bad (for instance
	// 5 > "banana" or true * 4.
	switch e.Operation {
	case "+":
		return ExpPlus(left, right)
	case "-":
		return ExpMinus(left, right)
	case "*":
		return ExpMultiply(left, right)
	case "/":
		return ExpDivide(left, right)
	case "==":
		return ExpEquals(left, right)
	case "!=":
		return ExpNotEquals(left, right)
	case "<":
		return ExpLT(left, right)
	case "<=":
		return ExpLTEq(left, right)
	case ">":
		return ExpGT(left, right)
	case ">=":
		return ExpGTEq(left, right)
	case "&&":
		return ExpBoolAnd(left, right)
	case "||":
		return ExpBoolOr(left, right)
	}

	return nil, fmt.Errorf(
		"Encountered unknown operation '%s' in expression at %s:%d",
		e.Operation, ls.definedInFile, e.LineNum,
	)
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
				"Unsupported argument '%s' sent to type at %s:%d",
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

func (ls *localState) resolveInterpolatedString(is InterpolatedString) (QuotedString, error) {
	return ls.resolveInterpolatedStringRecursive(
		is, nil, map[VariableName]bool{},
	)
}

func (ls *localState) resolveReferenceRecursive(r Reference, chain []*VariableDef, seenNames map[VariableName]bool) (Reference, error) {
	resolved, err := ls.resolveValueRecursive(
		r.Scalar, r.LineNum, chain, seenNames,
	)
	if err != nil {
		return r, nil
	}

	if str, ok := resolved.(QuotedString); !ok {
		return r, fmt.Errorf(
			"Reference keys must be strings (got %T) at %s:%d",
			resolved, ls.definedInFile, r.LineNum,
		)
	} else {
		r.Scalar = str
		return r, nil
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
