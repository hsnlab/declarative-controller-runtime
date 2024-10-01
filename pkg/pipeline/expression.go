package pipeline

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/json"
)

const ExpressionDumpMaxLevel = 10

type Unstructured = map[string]any

type evalCtx struct {
	object, subject any
	log             logr.Logger
}

type Expression struct {
	Op      string
	Arg     *Expression
	Literal any
}

func (e *Expression) Evaluate(ctx evalCtx) (any, error) {
	if len(e.Op) == 0 {
		return nil, NewInvalidArgumentsError(fmt.Sprintf("empty operator in expession %q", e.String()))
	}

	switch e.Op {
	case "@bool":
		lit := e.Literal
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}
			lit = v
		}

		v, err := asBool(lit)
		if err != nil {
			return nil, NewExpressionError(e, err)
		}

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", v)

		return v, nil

	case "@int":
		lit := e.Literal
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}
			lit = v
		}

		v, err := asInt(lit)
		if err != nil {
			return nil, NewExpressionError(e, err)
		}

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", v)

		return v, nil

	case "@float":
		lit := e.Literal
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}
			lit = v
		}

		v, err := asFloat(lit)
		if err != nil {
			return nil, NewExpressionError(e, err)
		}

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", v)

		return v, nil

	case "@string":
		lit := e.Literal
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}
			lit = v
		}

		str, err := asString(lit)
		if err != nil {
			return nil, NewExpressionError(e, err)
		}

		ret, err := e.GetJSONPath(ctx, str)
		if err != nil {
			return nil, err
		}

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", ret)

		return ret, nil

	case "@list":
		ret := []any{}
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}

			vs, ok := v.([]any)
			if !ok {
				return nil, NewExpressionError(e, errors.New("argument must be a list"))
			}

			ret = vs
		} else {
			// literal lists stored in Literal
			vs, ok := e.Literal.([]Expression)
			if !ok {
				return nil, NewExpressionError(e,
					errors.New("argument must be an expression list"))
			}

			for _, exp := range vs {
				res, err := exp.Evaluate(ctx)
				if err != nil {
					return nil, err
				}
				ret = append(ret, res)
			}
		}

		// WARNING: this will destroy multi-dimensional arrays
		ret = unpackList(ret)

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", ret)

		return ret, nil

	case "@dict":
		ret := Unstructured{}
		if e.Arg != nil {
			// eval stacked expressions stored in e.Arg
			v, err := e.Arg.Evaluate(ctx)
			if err != nil {
				return nil, err
			}

			// must be Unstructured
			vs, ok := v.(Unstructured)
			if !ok {
				return nil, NewExpressionError(e, errors.New("argument must be a map"))
			}
			ret = vs
		} else {
			// map stored as a Literal
			if reflect.ValueOf(e.Literal).Kind() != reflect.Map {
				return nil, NewExpressionError(e, errors.New("argument must be a map literal"))
			}

			vm, ok := e.Literal.(map[string]Expression)
			if !ok {
				return nil, NewExpressionError(e,
					errors.New("argument must be a string->expression map"))
			}

			for k, exp := range vm {
				// evaluate arguments
				res, err := exp.Evaluate(ctx)
				if err != nil {
					return nil, err
				}
				err = exp.SetJSONPath(ctx, k, res, ret)
				if err != nil {
					return nil, NewExpressionError(e,
						fmt.Errorf("could not deference JSON \"set\" expression: %w", err))
				}
			}
		}

		ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", ret)

		return ret, nil
	}

	// list commands: must eval the arg themselves
	if string(e.Op[0]) == "@" {
		switch e.Op {
		case "@filter":
			args, err := asExpOrList(e.Arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e,
					errors.New("invalid arguments: expected 2 arguments"))
			}

			// conditional
			cond := args[0]

			// arguments
			rawArg, err := args[1].Evaluate(ctx)
			if err != nil {
				return nil, errors.New("failed to evaluate arguments")
			}

			list, err := asList(rawArg)
			if err != nil {
				return nil, errors.New("invalid arguments: expected a list")
			}

			vs := []any{}
			for _, input := range list {
				res, err := cond.Evaluate(evalCtx{object: ctx.object, subject: input, log: ctx.log})
				if err != nil {
					return nil, err
				}

				b, err := asBool(res)
				if err != nil {
					return nil, NewExpressionError(e,
						fmt.Errorf("expected conditional expression to "+
							"evaluate to boolean: %w", err))
				}

				if b {
					vs = append(vs, input)
				}
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", vs)

			return vs, nil

		case "@any": // @in: [exp, list]
			args, err := asExpOrList(e.Arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e,
					errors.New("invalid arguments: expected 2 arguments"))
			}

			// conditional
			exp := args[0]

			// arguments
			rawArg, err := args[1].Evaluate(ctx)
			if err != nil {
				return nil, errors.New("failed to evaluate arguments")
			}

			list, err := asList(rawArg)
			if err != nil {
				return nil, errors.New("invalid arguments: expected a list")
			}

			v := false
			for _, input := range list {
				res, err := exp.Evaluate(evalCtx{object: ctx.object, subject: input, log: ctx.log})
				if err != nil {
					return nil, err
				}

				b, err := asBool(res)
				if err != nil {
					return nil, NewExpressionError(e,
						fmt.Errorf("expected conditional expression to "+
							"evaluate to boolean: %w", err))
				}

				if b {
					v = true
					break
				}
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

		case "@all": // @in: [exp, list]
			args, err := asExpOrList(e.Arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e,
					errors.New("invalid arguments: expected 2 arguments"))
			}

			// conditional
			exp := args[0]

			// arguments
			rawArg, err := args[1].Evaluate(ctx)
			if err != nil {
				return nil, errors.New("failed to evaluate arguments")
			}

			list, err := asList(rawArg)
			if err != nil {
				return nil, errors.New("invalid arguments: expected a list")
			}

			v := true
			for _, input := range list {
				res, err := exp.Evaluate(evalCtx{object: ctx.object, subject: input, log: ctx.log})
				if err != nil {
					return nil, err
				}

				b, err := asBool(res)
				if err != nil {
					return nil, NewExpressionError(e,
						fmt.Errorf("expected conditional expression to "+
							"evaluate to boolean: %w", err))
				}

				if !b {
					v = false
					break
				}
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

		case "@none": // @in: [exp, list]
			args, err := asExpOrList(e.Arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e,
					errors.New("invalid arguments: expected 2 arguments"))
			}

			// conditional
			exp := args[0]

			// arguments
			rawArg, err := args[1].Evaluate(ctx)
			if err != nil {
				return nil, errors.New("failed to evaluate arguments")
			}

			list, err := asList(rawArg)
			if err != nil {
				return nil, errors.New("invalid arguments: expected a list")
			}

			v := true
			for _, input := range list {
				res, err := exp.Evaluate(evalCtx{object: ctx.object, subject: input, log: ctx.log})
				if err != nil {
					return nil, err
				}

				b, err := asBool(res)
				if err != nil {
					return nil, NewExpressionError(e,
						fmt.Errorf("expected conditional expression to "+
							"evaluate to boolean: %w", err))
				}

				if b {
					v = false
					break
				}
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

		case "@map":
			args, err := asExpOrList(e.Arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			// function
			exp := args[0]

			// arguments
			rawArg, err := args[1].Evaluate(ctx)
			if err != nil {
				return nil, errors.New("failed to evaluate arguments")
			}

			list, err := asList(rawArg)
			if err != nil {
				return nil, errors.New("invalid arguments: expected a list")
			}

			vs := []any{}
			for _, input := range list {
				res, err := exp.Evaluate(evalCtx{object: ctx.object, subject: input, log: ctx.log})
				if err != nil {
					return nil, err
				}

				vs = append(vs, res)
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "result", vs)

			return vs, nil

			// case "@fold":
			// 	args, err := asExpOrList(e.Arg)
			// 	if err != nil {
			// 		return nil, NewExpressionError(e.Op, e.Raw, err)
			// 	}

			// 	if len(args) == 2 {
			// 		return nil, NewExpressionError(e.Op, e.Raw, errors.New("not enough arguments"))
			// 	}

			// 	var outputs []map[string]any
			// 	for _, exp := range args {
			// 		res, err := exp.Evaluate(eng)
			// 		if err != nil {
			// 			return nil, NewExpressionError(e.Op, e.Raw,
			// 				fmt.Errorf("should evaluate to an object list: %s", err))
			// 		}

			// 		outputs, err = asObjectList(eng.view, res)
			// 		if err != nil {
			// 			return nil, err
			// 		}

			// 		eng.inputs = outputs
			// 	}

			// 	eng.log.V(4).Info("eval ready", "expression", e.String(), "result", outputs)

			// 	return outputs, nil
		}
	}

	// operators
	// evaluate subexpression
	if e.Arg == nil {
		return nil, NewExpressionError(e, errors.New("empty argument list"))
	}

	arg, err := e.Arg.Evaluate(ctx)
	if err != nil {
		return nil, err
	}

	if string(e.Op[0]) == "@" {
		switch e.Op {
		// unary bool
		case "@isnil":
			v := arg == nil
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", arg, "result", v)
			return v, nil

		case "@exists":
			v := arg != nil
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", arg, "result", v)
			return v, nil

		case "@not":
			arg, err := asBool(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := !arg
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", arg, "result", v)
			return v, nil

		// binary bool
		case "@eq":
			args, err := asList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e, errors.New("expected 2 arguments"))
			}

			v := reflect.DeepEqual(args[0], args[1])
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", args, "result", v)
			return v, nil

			// list bool
		case "@and":
			args, err := asBoolList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := true
			for i := range args {
				v = v && args[i]
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", args, "result", v)

			return v, nil

		case "@or":
			args, err := asBoolList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := false
			for i := range args {
				v = v || args[i]
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", args, "result", v)

			return v, nil

			// binary
		case "@lt":
			is, fs, kind, err := asBinaryIntOrFloatList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			if kind == reflect.Int64 {
				v := is[0] < is[1]
				ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", is, "result", v)
				return v, nil
			}

			v := fs[0] < fs[1]
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", fs, "result", v)
			return v, nil

		case "@lte":
			is, fs, kind, err := asBinaryIntOrFloatList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			if kind == reflect.Int64 {
				v := is[0] <= is[1]
				ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", is, "result", v)
				return v, nil
			}

			v := fs[0] <= fs[1]
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", fs, "result", v)
			return v, nil

		case "@gt":
			is, fs, kind, err := asBinaryIntOrFloatList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			if kind == reflect.Int64 {
				v := is[0] > is[1]
				ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", is, "result", v)
				return v, nil
			}

			v := fs[0] > fs[1]
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", fs, "result", v)
			return v, nil

		case "@gte":
			is, fs, kind, err := asBinaryIntOrFloatList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			if kind == reflect.Int64 {
				v := is[0] >= is[1]
				ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", is, "result", v)
				return v, nil
			}

			v := fs[0] >= fs[1]
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", fs, "result", v)
			return v, nil

		case "@selector": // [selector, labels]
			args, err := asList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e,
					errors.New("invalid arguments: expected 2 arguments"))
			}

			if args[0] == nil || args[1] == nil {
				return false, nil
			}

			selector, err := asObject(args[0])
			if err != nil {
				return nil, NewExpressionError(e, fmt.Errorf("invalid label selector: %w", err))
			}

			labels, err := asObject(args[1])
			if err != nil {
				return nil, NewExpressionError(e, fmt.Errorf("invalid label set: %w", err))
			}

			// arguments
			res, err := MatchLabels(labels, selector)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate label selector: %w", err)
			}

			v, err := asBool(res)
			if err != nil {
				return nil, NewExpressionError(e, fmt.Errorf("expected label selector expression to "+
					"evaluate to boolean: %w", err))
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

			// unary arithmetic
		case "@abs":
			f, err := asFloat(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := math.Abs(f)
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", f, "result", v)
			return v, nil

		case "@ceil":
			f, err := asFloat(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := math.Ceil(f)
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", f, "result", v)
			return v, nil

		case "@floor":
			f, err := asFloat(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := math.Floor(f)
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "args", f, "result", v)
			return v, nil

			// list ops
		case "@sum":
			is, fs, kind, err := asIntOrFloatList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			var v any
			if kind == reflect.Int64 {
				vi := int64(0)
				for i := range is {
					vi += is[i]
				}
				v = vi
			} else {
				vf := 0.0
				for i := range fs {
					vf += fs[i]
				}
				v = vf
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", arg, "result", v)
			return v, nil

		case "@len":
			args, err := asList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			v := int64(len(args))
			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

		case "@in": // @in: [elem, list]
			args, err := asList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)

			}

			if len(args) != 2 {
				return nil, NewExpressionError(e, errors.New("expected 2 arguments"))
			}

			elem := args[0]
			list, err := asList(args[1])
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := false
			for i := range list {
				if reflect.DeepEqual(list[i], elem) {
					v = true
					break
				}
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)
			return v, nil

		case "@concat":
			args, err := asStringList(arg)
			if err != nil {
				return nil, NewExpressionError(e, err)
			}

			v := ""
			for i := range args {
				v += args[i]
			}

			ctx.log.V(8).Info("eval ready", "expression", e.String(), "arg", args, "result", v)

			return v, nil

		default:
			return nil, NewExpressionError(e, errors.New("unknown op"))
		}
	}

	// literal map
	return Unstructured{e.Op: arg}, nil
}

// unpacks the first-level list if any
func unpackList(a any) []any {
	v := reflect.ValueOf(a)

	// If it's not a slice, return nil
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return []any{a}
	}

	// If it's an empty slice, return nil
	if v.IsNil() || v.Len() == 0 {
		return []any{}
	}

	// If it's [][]any, return the first slice
	elemKind := v.Type().Elem().Kind()
	if elemKind == reflect.Slice || elemKind == reflect.Array {
		return v.Index(0).Interface().([]any)
	}

	// If it's []any{[]any, ...}, check if the first element is a slice
	first := v.Index(0)
	if !first.IsNil() {
		vs, ok := first.Interface().([]any)
		if ok {
			return vs
		}
	}

	return a.([]any)
}

func (e *Expression) UnmarshalJSON(b []byte) error {
	// cut raw content
	// try to unmarshal as a bool terminal expression
	bv := false
	if err := json.Unmarshal(b, &bv); err == nil {
		*e = Expression{Op: "@bool", Literal: bv}
		return nil
	}

	// try to unmarshal as an int terminal expression
	var iv int64 = 0
	if err := json.Unmarshal(b, &iv); err == nil {
		*e = Expression{Op: "@int", Literal: iv}
		return nil
	}

	// try to unmarshal as a float terminal expression
	fv := 0.0
	if err := json.Unmarshal(b, &fv); err == nil {
		*e = Expression{Op: "@float", Literal: fv}
		return nil
	}

	// try to unmarshal as a string terminal expression
	sv := ""
	if err := json.Unmarshal(b, &sv); err == nil && sv != "" {
		*e = Expression{Op: "@string", Literal: sv}
		return nil
	}

	// try to unmarshal as a literal list expression
	mv := []Expression{}
	if err := json.Unmarshal(b, &mv); err == nil {
		*e = Expression{Op: "@list", Literal: mv}
		return nil
	}

	// try to unmarshal as a map expression
	cv := map[string]Expression{}
	if err := json.Unmarshal(b, &cv); err == nil {
		// specialcase operators: an op has a single key that starts with @
		if len(cv) == 1 {
			op := ""
			for k := range cv {
				op = k
				break
			}
			if string(op[0]) == "@" {
				exp := cv[op]
				*e = Expression{Op: op, Arg: &exp}
				return nil
			}
		}

		// literal map: store as exp with op @dict and map as Literal
		*e = Expression{Op: "@dict", Literal: cv}
		return nil
	}

	return NewUnmarshalError("expression", string(b))
}

func (e *Expression) MarshalJSON() ([]byte, error) {
	switch e.Op {
	case "@any":
		return json.Marshal(e.Literal)

	case "@bool":
		if e.Arg != nil {
			// keep the op for a correct round-trip and possible side-effects (conversion)
			ret := map[string]*Expression{e.Op: e.Arg}
			return json.Marshal(ret)
		}
		v, err := asBool(e.Literal)
		if err != nil {
			return []byte(""), err
		}
		return json.Marshal(v)

	case "@int":
		if e.Arg != nil {
			// keep the op for a correct round-trip and possible side-effects (conversion)
			ret := map[string]*Expression{e.Op: e.Arg}
			return json.Marshal(ret)
		}
		v, err := asInt(e.Literal)
		if err != nil {
			return []byte(""), err
		}
		return json.Marshal(v)

	case "@float":
		if e.Arg != nil {
			// keep the op for a correct round-trip and possible side-effects (conversion)
			ret := map[string]*Expression{e.Op: e.Arg}
			return json.Marshal(ret)
		}
		v, err := asFloat(e.Literal)
		if err != nil {
			return []byte(""), err
		}
		return json.Marshal(v)

	case "@string":
		if e.Arg != nil {
			// keep the op for a correct round-trip and possible side-effects (conversion)
			ret := map[string]*Expression{e.Op: e.Arg}
			return json.Marshal(ret)
		}
		v, err := asString(e.Literal)
		if err != nil {
			return []byte(""), err
		}
		return json.Marshal(v)

	case "@list":
		if e.Arg != nil {
			return json.Marshal(e.Arg)
		}
		es, ok := e.Literal.([]Expression)
		if !ok {
			return []byte(""), fmt.Errorf("invalid expression list: %#v", e)
		}
		return json.Marshal(es)

	case "@dict":
		if e.Arg != nil {
			// can this ever happen?
			return json.Marshal(e.Arg)
		}

		es, ok := e.Literal.(map[string]Expression)
		if !ok {
			return []byte(""), fmt.Errorf("invalid expression map: %#v", e)
		}
		// this is terribly stupid but here we go
		em := map[string]*Expression{}
		for k, v := range es {
			v := v
			em[k] = &v
		}
		return json.Marshal(em)

	default:
		// everything else is a valid op
		if e.Op[0] != '@' {
			return []byte(""), fmt.Errorf("expected an op starting with @, got %#v", e)
		}

		ret := map[string]*Expression{e.Op: e.Arg}
		return json.Marshal(ret)
	}
	// return []byte(""), fmt.Errorf("failed to JSON marshal expression %#v", e)
}

func (e *Expression) String() string {
	b, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	return string(b)
}

// func (e *Expression) string(level int) string {
// 	if level >= ExpressionDumpMaxLevel {
// 		return "..."
// 	}

// 	// literals
// 	switch e.Op {
// 	case "@any":
// 		return fmt.Sprintf("%#v", e.Literal)
// 	case "@bool":
// 		if e.Arg != nil {
// 			return e.Arg.string(level + 1)
// 		}
// 		v, err := asBool(e.Literal)
// 		if err != nil {
// 			return "<invalid>"
// 		}
// 		return fmt.Sprintf("%t", v)
// 	case "@int":
// 		if e.Arg != nil {
// 			return e.Arg.string(level + 1)
// 		}
// 		v, err := asInt(e.Literal)
// 		if err != nil {
// 			return "<invalid>"
// 		}
// 		return fmt.Sprintf("%d", v)
// 	case "@float":
// 		if e.Arg != nil {
// 			return e.Arg.string(level + 1)
// 		}
// 		v, err := asFloat(e.Literal)
// 		if err != nil {
// 			return "<invalid>"
// 		}
// 		return fmt.Sprintf("%.3f", v)
// 	case "@string":
// 		if e.Arg != nil {
// 			return e.Arg.string(level + 1)
// 		}
// 		v, err := asString(e.Literal)
// 		if err != nil {
// 			return "<invalid>"
// 		}
// 		return fmt.Sprintf("%s", v)
// 	case "@list":
// 		if e.Arg != nil {
// 			return e.Arg.string(level + 1)
// 		}
// 		es, ok := e.Literal.([]Expression)
// 		if !ok {
// 			return "<invalid>"
// 		}
// 		return fmt.Sprintf("%s", strings.Join(util.Map(
// 			func(exp Expression) string { return exp.string(level + 1) }, es,
// 		), ","))
// 	case "@dict":
// 		if e.Arg != nil {
// 			return fmt.Sprintf("{%s}", e.Arg.string(level+1))
// 		}
// 		return fmt.Sprintf("{%s}", stringifier(e.Literal, level+1))
// 	}

// 	return fmt.Sprintf("%s:[%s]", e.Op, e.Arg.string(level+1))
// }

// func stringify(arg any) string {
// 	return stringifier(arg, 0)
// }

// func stringifier(arg any, level int) string {
// 	if level >= ExpressionDumpMaxLevel {
// 		return "..."
// 	}

// 	switch v := arg.(type) {
// 	case Expression:
// 		return v.string(level + 1)
// 	case *Expression:
// 		return v.string(level + 1)
// 	case bool, int64, float64, string:
// 		return fmt.Sprintf("%s", v)
// 	case []any:
// 		ret := []string{}
// 		for _, item := range v {
// 			ret = append(ret, stringifier(item, level+1))
// 		}
// 		return fmt.Sprintf("[%s]", strings.Join(ret, ","))
// 	case map[string]Expression:
// 		ret := []string{}
// 		for key, value := range v {
// 			ret = append(ret, fmt.Sprintf("%s:%s", key, value.string(level+1)))
// 		}
// 		return fmt.Sprintf("{%s}", strings.Join(ret, ","))
// 	case map[string]any:
// 		ret := []string{}
// 		for key, value := range v {
// 			ret = append(ret, fmt.Sprintf("%s:%s", key, stringifier(value, level+1)))
// 		}
// 		return fmt.Sprintf("{%s}", strings.Join(ret, ","))
// 	default:
// 		return fmt.Sprintf("%s", v)
// 	}
// }
