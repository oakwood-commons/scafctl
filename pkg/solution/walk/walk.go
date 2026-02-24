// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package walk provides a visitor/walker pattern for traversing solution
// structures. It eliminates duplicated traversal logic across lint, dependency
// extraction, explain, and MCP tools by providing a single canonical Walk
// function with composable visitor callbacks.
package walk

import (
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Visitor defines optional callbacks for each node type in the solution tree.
// All fields are optional -- set only the callbacks you need.
// Return a non-nil error from any callback to abort the walk immediately.
type Visitor struct {
	Solution           func(sol *solution.Solution) error
	Metadata           func(path string, meta *solution.Metadata) error
	Catalog            func(path string, cat *solution.Catalog) error
	Spec               func(path string, sp *solution.Spec) error
	Resolver           func(path, name string, r *resolver.Resolver) error
	ResolvePhase       func(path, resolverName string, rp *resolver.ResolvePhase) error
	TransformPhase     func(path, resolverName string, tp *resolver.TransformPhase) error
	ValidatePhase      func(path, resolverName string, vp *resolver.ValidatePhase) error
	ProviderSource     func(path string, ps *resolver.ProviderSource) error
	ProviderTransform  func(path string, pt *resolver.ProviderTransform) error
	ProviderValidation func(path string, pv *resolver.ProviderValidation) error
	Action             func(path, name, section string, act *action.Action) error
	Workflow           func(path string, w *action.Workflow) error
	ValueRef           func(path string, vr *spec.ValueRef) error
	Condition          func(path, conditionKind string, expr *celexp.Expression) error
	ForEach            func(path string, fe *spec.ForEachClause) error
	TestSuite          func(path string, ts *soltesting.TestSuite) error
	TestCase           func(path, name string, tc *soltesting.TestCase) error
}

// Walk traverses the solution tree calling visitor callbacks in depth-first order.
// Map iterations (resolvers, actions, tests) are sorted by key for deterministic traversal.
func Walk(sol *solution.Solution, v *Visitor) error {
	if sol == nil || v == nil {
		return nil
	}

	if v.Solution != nil {
		if err := v.Solution(sol); err != nil {
			return err
		}
	}

	if v.Metadata != nil {
		if err := v.Metadata("metadata", &sol.Metadata); err != nil {
			return err
		}
	}

	if v.Catalog != nil {
		if err := v.Catalog("catalog", &sol.Catalog); err != nil {
			return err
		}
	}

	if v.Spec != nil {
		if err := v.Spec("spec", &sol.Spec); err != nil {
			return err
		}
	}

	if err := walkResolvers(sol, v); err != nil {
		return err
	}

	if err := walkWorkflow(sol, v); err != nil {
		return err
	}

	return walkTesting(sol, v)
}

func walkResolvers(sol *solution.Solution, v *Visitor) error {
	if !sol.Spec.HasResolvers() {
		return nil
	}

	names := sortedKeys(sol.Spec.Resolvers)
	for _, name := range names {
		r := sol.Spec.Resolvers[name]
		if r == nil {
			continue
		}

		path := "spec.resolvers." + name

		if v.Resolver != nil {
			if err := v.Resolver(path, name, r); err != nil {
				return err
			}
		}

		if r.When != nil && r.When.Expr != nil {
			if err := visitCondition(v, path+".when", "when", r.When.Expr); err != nil {
				return err
			}
		}

		if r.Resolve != nil {
			if err := walkResolvePhase(v, path+".resolve", name, r.Resolve); err != nil {
				return err
			}
		}

		if r.Transform != nil {
			if err := walkTransformPhase(v, path+".transform", name, r.Transform); err != nil {
				return err
			}
		}

		if r.Validate != nil {
			if err := walkValidatePhase(v, path+".validate", name, r.Validate); err != nil {
				return err
			}
		}
	}

	return nil
}

func walkResolvePhase(v *Visitor, resolvePath, resolverName string, rp *resolver.ResolvePhase) error {
	if v.ResolvePhase != nil {
		if err := v.ResolvePhase(resolvePath, resolverName, rp); err != nil {
			return err
		}
	}

	if rp.When != nil && rp.When.Expr != nil {
		if err := visitCondition(v, resolvePath+".when", "when", rp.When.Expr); err != nil {
			return err
		}
	}
	if rp.Until != nil && rp.Until.Expr != nil {
		if err := visitCondition(v, resolvePath+".until", "until", rp.Until.Expr); err != nil {
			return err
		}
	}

	for i := range rp.With {
		src := &rp.With[i]
		srcPath := fmt.Sprintf("%s.with[%d]", resolvePath, i)

		if v.ProviderSource != nil {
			if err := v.ProviderSource(srcPath, src); err != nil {
				return err
			}
		}

		if src.When != nil && src.When.Expr != nil {
			if err := visitCondition(v, srcPath+".when", "when", src.When.Expr); err != nil {
				return err
			}
		}

		if err := walkInputs(v, srcPath+".inputs", src.Inputs); err != nil {
			return err
		}
	}

	return nil
}

func walkTransformPhase(v *Visitor, transformPath, resolverName string, tp *resolver.TransformPhase) error {
	if v.TransformPhase != nil {
		if err := v.TransformPhase(transformPath, resolverName, tp); err != nil {
			return err
		}
	}

	if tp.When != nil && tp.When.Expr != nil {
		if err := visitCondition(v, transformPath+".when", "when", tp.When.Expr); err != nil {
			return err
		}
	}

	for i := range tp.With {
		t := &tp.With[i]
		tPath := fmt.Sprintf("%s.with[%d]", transformPath, i)

		if v.ProviderTransform != nil {
			if err := v.ProviderTransform(tPath, t); err != nil {
				return err
			}
		}

		if t.When != nil && t.When.Expr != nil {
			if err := visitCondition(v, tPath+".when", "when", t.When.Expr); err != nil {
				return err
			}
		}

		if t.ForEach != nil {
			if err := walkForEach(v, tPath+".forEach", t.ForEach); err != nil {
				return err
			}
		}

		if err := walkInputs(v, tPath+".inputs", t.Inputs); err != nil {
			return err
		}
	}

	return nil
}

func walkValidatePhase(v *Visitor, validatePath, resolverName string, vp *resolver.ValidatePhase) error {
	if v.ValidatePhase != nil {
		if err := v.ValidatePhase(validatePath, resolverName, vp); err != nil {
			return err
		}
	}

	if vp.When != nil && vp.When.Expr != nil {
		if err := visitCondition(v, validatePath+".when", "when", vp.When.Expr); err != nil {
			return err
		}
	}

	for i := range vp.With {
		pv := &vp.With[i]
		pvPath := fmt.Sprintf("%s.with[%d]", validatePath, i)

		if v.ProviderValidation != nil {
			if err := v.ProviderValidation(pvPath, pv); err != nil {
				return err
			}
		}

		if err := walkInputs(v, pvPath+".inputs", pv.Inputs); err != nil {
			return err
		}

		if pv.Message != nil {
			if err := visitValueRef(v, pvPath+".message", pv.Message); err != nil {
				return err
			}
		}
	}

	return nil
}

func walkWorkflow(sol *solution.Solution, v *Visitor) error {
	if sol.Spec.Workflow == nil {
		return nil
	}
	w := sol.Spec.Workflow

	if v.Workflow != nil {
		if err := v.Workflow("spec.workflow", w); err != nil {
			return err
		}
	}

	if err := walkActions(v, "spec.workflow.actions", "actions", w.Actions); err != nil {
		return err
	}

	return walkActions(v, "spec.workflow.finally", "finally", w.Finally)
}

func walkActions(v *Visitor, basePath, section string, actions map[string]*action.Action) error {
	if len(actions) == 0 {
		return nil
	}

	names := sortedKeys(actions)
	for _, name := range names {
		act := actions[name]
		if act == nil {
			continue
		}

		path := basePath + "." + name

		if v.Action != nil {
			if err := v.Action(path, name, section, act); err != nil {
				return err
			}
		}

		if act.When != nil && act.When.Expr != nil {
			if err := visitCondition(v, path+".when", "when", act.When.Expr); err != nil {
				return err
			}
		}

		if act.ForEach != nil {
			if err := walkForEach(v, path+".forEach", act.ForEach); err != nil {
				return err
			}
		}

		if act.Retry != nil && act.Retry.RetryIf != nil {
			if err := visitCondition(v, path+".retry.retryIf", "when", act.Retry.RetryIf); err != nil {
				return err
			}
		}

		if err := walkInputs(v, path+".inputs", act.Inputs); err != nil {
			return err
		}
	}

	return nil
}

func walkTesting(sol *solution.Solution, v *Visitor) error {
	if sol.Spec.Testing == nil {
		return nil
	}

	ts := sol.Spec.Testing

	if v.TestSuite != nil {
		if err := v.TestSuite("spec.testing", ts); err != nil {
			return err
		}
	}

	if ts.Cases == nil {
		return nil
	}

	names := sortedKeys(ts.Cases)
	for _, name := range names {
		tc := ts.Cases[name]
		if tc == nil {
			continue
		}

		path := "spec.testing.cases." + name

		if v.TestCase != nil {
			if err := v.TestCase(path, name, tc); err != nil {
				return err
			}
		}
	}

	return nil
}

func walkInputs(v *Visitor, basePath string, inputs map[string]*spec.ValueRef) error {
	if len(inputs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		vr := inputs[key]
		if vr == nil {
			continue
		}
		if err := visitValueRef(v, basePath+"."+key, vr); err != nil {
			return err
		}
	}

	return nil
}

func walkForEach(v *Visitor, path string, fe *spec.ForEachClause) error {
	if fe == nil {
		return nil
	}

	if v.ForEach != nil {
		if err := v.ForEach(path, fe); err != nil {
			return err
		}
	}

	if fe.In != nil {
		if err := visitValueRef(v, path+".in", fe.In); err != nil {
			return err
		}
	}

	return nil
}

func visitValueRef(v *Visitor, path string, vr *spec.ValueRef) error {
	if v.ValueRef == nil || vr == nil {
		return nil
	}
	return v.ValueRef(path, vr)
}

func visitCondition(v *Visitor, path, kind string, expr *celexp.Expression) error {
	if v.Condition == nil || expr == nil {
		return nil
	}
	return v.Condition(path, kind, expr)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
