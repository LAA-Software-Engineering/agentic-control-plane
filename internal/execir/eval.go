package execir

import (
	"fmt"
	"reflect"
)

// evalArgs resolves each argument value against scope.
func evalArgs(scope map[string]any, args map[string]Value) (map[string]any, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		val, err := evalValue(scope, v)
		if err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

// evalValue resolves a Ref against the scope or returns a Lit's Go value.
func evalValue(scope map[string]any, v Value) (any, error) {
	switch x := v.(type) {
	case Lit:
		return x.V, nil
	case Ref:
		return resolvePath(scope, x.Path)
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("execir: unknown value %T", v)
	}
}

// resolvePath resolves a dotted path against scope. The head must be a bound
// name (an unbound head is a programming error the checker/lowering should have
// caught, so it is surfaced loudly); a missing NESTED field resolves to nil
// (gradual — agent and tool outputs are dynamically shaped).
func resolvePath(scope map[string]any, path []string) (any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("execir: empty reference path")
	}
	cur, ok := scope[path[0]]
	if !ok {
		return nil, fmt.Errorf("execir: unresolved reference %q", path[0])
	}
	for _, seg := range path[1:] {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, nil
		}
		cur = m[seg]
	}
	return cur, nil
}

// evalCollection resolves a value expected to be an iterable and returns its
// elements. A JSON array ([]any) iterates its elements; nil is an empty
// collection (an absent field yields zero iterations rather than an error). Any
// other concrete type is a runtime error — a loop needs a collection.
func evalCollection(scope map[string]any, v Value) ([]any, error) {
	val, err := evalValue(scope, v)
	if err != nil {
		return nil, err
	}
	switch xs := val.(type) {
	case nil:
		return nil, nil
	case []any:
		return xs, nil
	default:
		return nil, fmt.Errorf("execir: loop collection is %T, not a list", val)
	}
}

// evalExpr evaluates a boolean condition tree.
func evalExpr(scope map[string]any, e Expr) (bool, error) {
	switch x := e.(type) {
	case nil:
		return false, fmt.Errorf("execir: nil condition")
	case Leaf:
		val, err := evalValue(scope, x.V)
		if err != nil {
			return false, err
		}
		return truthy(val), nil
	case Not:
		b, err := evalExpr(scope, x.X)
		if err != nil {
			return false, err
		}
		return !b, nil
	case BinOp:
		return evalBinOp(scope, x)
	default:
		return false, fmt.Errorf("execir: unknown condition %T", e)
	}
}

func evalBinOp(scope map[string]any, x BinOp) (bool, error) {
	// Logical connectives short-circuit and operate on boolean sub-conditions.
	switch x.Op {
	case "&&":
		l, err := evalExpr(scope, x.X)
		if err != nil || !l {
			return false, err
		}
		return evalExpr(scope, x.Y)
	case "||":
		l, err := evalExpr(scope, x.X)
		if err != nil {
			return false, err
		}
		if l {
			return true, nil
		}
		return evalExpr(scope, x.Y)
	}
	// Comparisons operate on values.
	lv, err := leafValue(scope, x.X)
	if err != nil {
		return false, err
	}
	rv, err := leafValue(scope, x.Y)
	if err != nil {
		return false, err
	}
	switch x.Op {
	case "==":
		return valuesEqual(lv, rv), nil
	case "!=":
		return !valuesEqual(lv, rv), nil
	case "<", "<=", ">", ">=":
		return compareOrdered(x.Op, lv, rv)
	default:
		return false, fmt.Errorf("execir: unknown operator %q", x.Op)
	}
}

// leafValue evaluates an expression that must be a value leaf (the operand of a
// comparison). The parser only ever places Leaf nodes under a comparison, so a
// non-leaf here is an internal lowering error.
func leafValue(scope map[string]any, e Expr) (any, error) {
	leaf, ok := e.(Leaf)
	if !ok {
		return nil, fmt.Errorf("execir: comparison operand is not a value")
	}
	return evalValue(scope, leaf.V)
}

// truthy defines the boolean coercion for a bare condition leaf (`if flag`):
// booleans are themselves, null/absent is false, everything present is true.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	default:
		return true
	}
}

// valuesEqual is a total, panic-free equality over every value this IR can
// carry, so `==` is defined on the JSON objects and arrays a workflow input
// holds — a bare `a == b` on `any` would panic ("comparing uncomparable type")
// for a map or slice operand. Numbers compare numerically across int64/float64
// (1 == 1.0), strings/bools by value, arrays and objects structurally (element-
// and key-wise, recursively, with the same numeric normalization), and anything
// else via reflect.DeepEqual, which never panics. A type mismatch is unequal.
func valuesEqual(a, b any) bool {
	if af, aok := toFloat(a); aok {
		bf, bok := toFloat(b)
		return bok && af == bf
	}
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, va := range av {
			vb, present := bv[k]
			if !present || !valuesEqual(va, vb) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// compareOrdered evaluates <, <=, >, >= over numeric operands. A non-numeric
// operand is an error — ordering strings or booleans is not defined in the
// surface.
func compareOrdered(op string, a, b any) (bool, error) {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if !aok || !bok {
		return false, fmt.Errorf("execir: operator %q needs numeric operands, got %T and %T", op, a, b)
	}
	switch op {
	case "<":
		return af < bf, nil
	case "<=":
		return af <= bf, nil
	case ">":
		return af > bf, nil
	case ">=":
		return af >= bf, nil
	}
	return false, fmt.Errorf("execir: unknown operator %q", op)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	default:
		return 0, false
	}
}
