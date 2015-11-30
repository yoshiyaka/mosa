package resolver

import (
	"strings"

	. "github.com/yoshiyaka/mosa/ast"
)

func ExpPlus(a, b Value) (Value, error) {
	switch a.(type) {
	case int:
		return a.(int) + b.(int), nil
	case QuotedString:
		return a.(QuotedString) + b.(QuotedString), nil
	}

	panic("Bad types")
}

func ExpMinus(a, b Value) (Value, error) {
	switch a.(type) {
	case int:
		return a.(int) - b.(int), nil
	}

	panic("Bad types")
}

func ExpMultiply(a, b Value) (Value, error) {
	switch a.(type) {
	case int:
		return a.(int) * b.(int), nil
	}

	panic("Bad types")
}

func ExpDivide(a, b Value) (Value, error) {
	switch a.(type) {
	case int:
		return a.(int) / b.(int), nil
	}

	panic("Bad types")
}

func ExpEquals(a, b Value) (Bool, error) {
	switch a.(type) {
	case int:
		return a.(int) == b.(int), nil
	case QuotedString:
		return a.(QuotedString) == b.(QuotedString), nil
	case string:
		return a.(string) == b.(string), nil
	}

	panic("Bad types")
}

func ExpNotEquals(a, b Value) (Bool, error) {
	b, err := ExpEquals(a, b)
	return Bool(!b.(bool)), err
}

func ExpLT(a, b Value) (bool, error) {
	switch a.(type) {
	case int:
		return a.(int) < b.(int), nil
	case string:
		return strings.Compare(a.(string), b.(string)) < 0, nil
	}

	panic("Bad types")
}

func ExpLTEq(a, b Value) (bool, error) {
	switch a.(type) {
	case int:
		return a.(int) <= b.(int), nil
	case string:
		return strings.Compare(a.(string), b.(string)) <= 0, nil
	}

	panic("Bad types")
}

func ExpGT(a, b Value) (bool, error) {
	switch a.(type) {
	case int:
		return a.(int) > b.(int), nil
	case string:
		return strings.Compare(a.(string), b.(string)) > 0, nil
	}

	panic("Bad types")
}

func ExpGTEq(a, b Value) (bool, error) {
	switch a.(type) {
	case int:
		return a.(int) > b.(int), nil
	case string:
		return strings.Compare(a.(string), b.(string)) >= 0, nil
	}

	panic("Bad types")
}

func ExpBoolAnd(a, b Value) (bool, error) {
	switch a.(type) {
	case bool:
		return a.(bool) && b.(bool), nil
	}

	panic("Bad types")
}

func ExpBoolOr(a, b Value) (bool, error) {
	switch a.(type) {
	case bool:
		return a.(bool) || b.(bool), nil
	}

	panic("Bad types")
}