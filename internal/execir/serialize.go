package execir

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ProgramFormatV1 versions the serialized-program payload. An unknown version
// must fail loudly rather than be reinterpreted (SOUNDNESS.md S8): the pinned
// program is execution authority, so a decoder that guessed at an incompatible
// encoding would run something other than what was pinned.
const ProgramFormatV1 = "agentic.dev/execir/v1"

// MarshalPrograms serializes a name→Program map into a canonical, format-versioned
// JSON payload for the deployment snapshot's execution_ir artifact (issue #260).
// It round-trips through [UnmarshalPrograms] to a program with an identical
// [Program.Digest]; positions are omitted (diagnostic-only, already excluded from
// the digest).
func MarshalPrograms(programs map[string]*Program) ([]byte, error) {
	w := programsWire{FormatVersion: ProgramFormatV1, Programs: make(map[string]programWire, len(programs))}
	for name, p := range programs {
		if p == nil {
			continue
		}
		w.Programs[name] = programWire{
			Workflow: p.Workflow,
			Params:   p.Params,
			Body:     wireNodes(p.Body),
		}
	}
	return json.Marshal(w)
}

// UnmarshalPrograms decodes a payload written by [MarshalPrograms], failing on an
// unknown format version.
func UnmarshalPrograms(payload []byte) (map[string]*Program, error) {
	var w programsWire
	if err := json.Unmarshal(payload, &w); err != nil {
		return nil, fmt.Errorf("execir: decode programs: %w", err)
	}
	if w.FormatVersion != ProgramFormatV1 {
		return nil, fmt.Errorf("execir: unsupported program format %q (want %q)", w.FormatVersion, ProgramFormatV1)
	}
	out := make(map[string]*Program, len(w.Programs))
	for name, pw := range w.Programs {
		out[name] = &Program{Workflow: pw.Workflow, Params: pw.Params, Body: decodeNodes(pw.Body)}
	}
	return out, nil
}

// --- wire types -------------------------------------------------------------

type programsWire struct {
	FormatVersion string                 `json:"formatVersion"`
	Programs      map[string]programWire `json:"programs"`
}

type programWire struct {
	Workflow string     `json:"workflow"`
	Params   []string   `json:"params,omitempty"`
	Body     []nodeWire `json:"body,omitempty"`
}

// nodeWire is a flat tagged union: Kind selects which fields are meaningful.
type nodeWire struct {
	Kind       string             `json:"kind"`
	Bind       string             `json:"bind,omitempty"`
	Uses       string             `json:"uses,omitempty"`
	Agent      string             `json:"agent,omitempty"`
	Workflow   string             `json:"workflow,omitempty"`
	Args       map[string]valWire `json:"args,omitempty"`
	Value      *valWire           `json:"value,omitempty"`
	Cond       *exprWire          `json:"cond,omitempty"`
	Then       []nodeWire         `json:"then,omitempty"`
	Else       []nodeWire         `json:"else,omitempty"`
	Branches   []forkBranchWire   `json:"branches,omitempty"`
	Var        string             `json:"var,omitempty"`
	Parallel   bool               `json:"parallel,omitempty"`
	Collection *valWire           `json:"collection,omitempty"`
	Limit      int                `json:"limit,omitempty"`
	Body       []nodeWire         `json:"body,omitempty"`
	Nodes      []graphNodeWire    `json:"nodes,omitempty"`
	Desc       string             `json:"desc,omitempty"`
	RedactKeys []string           `json:"redactKeys,omitempty"`
}

type forkBranchWire struct {
	Bind  string     `json:"bind,omitempty"`
	Nodes []nodeWire `json:"nodes,omitempty"`
}

type graphNodeWire struct {
	ID    string   `json:"id"`
	Needs []string `json:"needs,omitempty"`
	Run   nodeWire `json:"run"`
}

type valWire struct {
	Kind   string      `json:"kind"`
	Path   []string    `json:"path,omitempty"`
	LitT   string      `json:"litType,omitempty"` // s | i | f | b | nil
	LitV   any         `json:"v,omitempty"`
	Fields []fieldWire `json:"fields,omitempty"`
	Elems  []valWire   `json:"elems,omitempty"`
	Parts  []valWire   `json:"parts,omitempty"`
}

type fieldWire struct {
	Key string  `json:"key"`
	Val valWire `json:"val"`
}

type exprWire struct {
	Kind string    `json:"kind"` // leaf | not | binop
	V    *valWire  `json:"v,omitempty"`
	X    *exprWire `json:"x,omitempty"`
	Y    *exprWire `json:"y,omitempty"`
	Op   string    `json:"op,omitempty"`
}

// --- encode -----------------------------------------------------------------

func wireNodes(nodes []Node) []nodeWire {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]nodeWire, len(nodes))
	for i, n := range nodes {
		out[i] = wireNode(n)
	}
	return out
}

func wireNode(n Node) nodeWire {
	switch v := n.(type) {
	case *InvokeTool:
		return nodeWire{Kind: "invokeTool", Bind: v.Bind, Uses: v.Uses, Args: wireArgs(v.Args)}
	case *InvokeAgent:
		return nodeWire{Kind: "invokeAgent", Bind: v.Bind, Agent: v.Agent, Args: wireArgs(v.Args)}
	case *InvokeWorkflow:
		return nodeWire{Kind: "invokeWorkflow", Bind: v.Bind, Workflow: v.Workflow, Args: wireArgs(v.Args)}
	case *Let:
		vw := wireVal(v.Value)
		return nodeWire{Kind: "let", Bind: v.Bind, Value: &vw}
	case *Branch:
		ew := wireExpr(v.Cond)
		return nodeWire{Kind: "branch", Cond: &ew, Then: wireNodes(v.Then), Else: wireNodes(v.Else)}
	case *Fork:
		brs := make([]forkBranchWire, len(v.Branches))
		for i, b := range v.Branches {
			brs[i] = forkBranchWire{Bind: b.Bind, Nodes: wireNodes(b.Nodes)}
		}
		return nodeWire{Kind: "fork", Branches: brs}
	case *Loop:
		cw := wireVal(v.Collection)
		return nodeWire{Kind: "loop", Var: v.Var, Parallel: v.Parallel, Collection: &cw, Body: wireNodes(v.Body)}
	case *While:
		ew := wireExpr(v.Cond)
		return nodeWire{Kind: "while", Cond: &ew, Limit: v.Limit, Body: wireNodes(v.Body)}
	case *Retry:
		ew := wireExpr(v.Cond)
		return nodeWire{Kind: "retry", Cond: &ew, Limit: v.Limit, Body: wireNodes(v.Body)}
	case *Return:
		vw := wireVal(v.Value)
		return nodeWire{Kind: "return", Value: &vw}
	case *Graph:
		gns := make([]graphNodeWire, len(v.Nodes))
		for i, gn := range v.Nodes {
			gns[i] = graphNodeWire{ID: gn.ID, Needs: gn.Needs, Run: wireNode(gn.Run)}
		}
		return nodeWire{Kind: "graph", Nodes: gns}
	case *Approval:
		return nodeWire{Kind: "approval", Bind: v.Bind, Desc: v.Description, RedactKeys: v.RedactKeys, Args: wireArgs(v.Args)}
	default:
		return nodeWire{Kind: "unknown"}
	}
}

func wireArgs(args map[string]Value) map[string]valWire {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]valWire, len(args))
	for k, v := range args {
		out[k] = wireVal(v)
	}
	return out
}

func wireVal(v Value) valWire {
	switch x := v.(type) {
	case Ref:
		return valWire{Kind: "ref", Path: x.Path}
	case Lit:
		return wireLit(x.V)
	case Object:
		fs := make([]fieldWire, len(x.Fields))
		for i, f := range x.Fields {
			fs[i] = fieldWire{Key: f.Key, Val: wireVal(f.Val)}
		}
		return valWire{Kind: "object", Fields: fs}
	case List:
		es := make([]valWire, len(x.Elems))
		for i, e := range x.Elems {
			es[i] = wireVal(e)
		}
		return valWire{Kind: "list", Elems: es}
	case Template:
		ps := make([]valWire, len(x.Parts))
		for i, p := range x.Parts {
			ps[i] = wireVal(p)
		}
		return valWire{Kind: "template", Parts: ps}
	case nil:
		return valWire{Kind: "lit", LitT: "nil"}
	default:
		return valWire{Kind: "lit", LitT: "nil"}
	}
}

func wireLit(v any) valWire {
	switch x := v.(type) {
	case string:
		return valWire{Kind: "lit", LitT: "s", LitV: x}
	case int64:
		// Encode as a decimal STRING, never a JSON number: json.Unmarshal decodes
		// every JSON number into float64 when the target is `any`, rounding an
		// int64 past 2^53 to the nearest double. That would silently hydrate a
		// different program than was pinned (the content digest guards the bytes,
		// not the decoded program). A string is lossless across the full int64.
		return valWire{Kind: "lit", LitT: "i", LitV: strconv.FormatInt(x, 10)}
	case float64:
		return valWire{Kind: "lit", LitT: "f", LitV: x}
	case bool:
		return valWire{Kind: "lit", LitT: "b", LitV: x}
	case nil:
		return valWire{Kind: "lit", LitT: "nil"}
	default:
		// A non-canonical scalar should not reach here (lowering canonicalizes),
		// but preserve it as a string so the payload stays total.
		return valWire{Kind: "lit", LitT: "s", LitV: fmt.Sprintf("%v", x)}
	}
}

func wireExpr(e Expr) exprWire {
	switch x := e.(type) {
	case Leaf:
		vw := wireVal(x.V)
		return exprWire{Kind: "leaf", V: &vw}
	case Not:
		xw := wireExpr(x.X)
		return exprWire{Kind: "not", X: &xw}
	case BinOp:
		xw := wireExpr(x.X)
		yw := wireExpr(x.Y)
		return exprWire{Kind: "binop", Op: x.Op, X: &xw, Y: &yw}
	default:
		return exprWire{Kind: "leaf", V: &valWire{Kind: "lit", LitT: "nil"}}
	}
}

// --- decode -----------------------------------------------------------------

func decodeNodes(nodes []nodeWire) []Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if dn := decodeNode(n); dn != nil {
			out = append(out, dn)
		}
	}
	return out
}

func decodeNode(n nodeWire) Node {
	switch n.Kind {
	case "invokeTool":
		return &InvokeTool{Bind: n.Bind, Uses: n.Uses, Args: decodeArgs(n.Args)}
	case "invokeAgent":
		return &InvokeAgent{Bind: n.Bind, Agent: n.Agent, Args: decodeArgs(n.Args)}
	case "invokeWorkflow":
		return &InvokeWorkflow{Bind: n.Bind, Workflow: n.Workflow, Args: decodeArgs(n.Args)}
	case "let":
		return &Let{Bind: n.Bind, Value: decodeValPtr(n.Value)}
	case "branch":
		return &Branch{Cond: decodeExprPtr(n.Cond), Then: decodeNodes(n.Then), Else: decodeNodes(n.Else)}
	case "fork":
		brs := make([]ForkBranch, len(n.Branches))
		for i, b := range n.Branches {
			brs[i] = ForkBranch{Bind: b.Bind, Nodes: decodeNodes(b.Nodes)}
		}
		return &Fork{Branches: brs}
	case "loop":
		return &Loop{Var: n.Var, Parallel: n.Parallel, Collection: decodeValPtr(n.Collection), Body: decodeNodes(n.Body)}
	case "while":
		return &While{Cond: decodeExprPtr(n.Cond), Limit: n.Limit, Body: decodeNodes(n.Body)}
	case "retry":
		return &Retry{Cond: decodeExprPtr(n.Cond), Limit: n.Limit, Body: decodeNodes(n.Body)}
	case "return":
		return &Return{Value: decodeValPtr(n.Value)}
	case "graph":
		gns := make([]GraphNode, len(n.Nodes))
		for i, gn := range n.Nodes {
			gns[i] = GraphNode{ID: gn.ID, Needs: gn.Needs, Run: decodeNode(gn.Run)}
		}
		return &Graph{Nodes: gns}
	case "approval":
		return &Approval{Bind: n.Bind, Description: n.Desc, RedactKeys: n.RedactKeys, Args: decodeArgs(n.Args)}
	default:
		return nil
	}
}

func decodeArgs(args map[string]valWire) map[string]Value {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]Value, len(args))
	for k, v := range args {
		out[k] = decodeVal(v)
	}
	return out
}

func decodeValPtr(v *valWire) Value {
	if v == nil {
		return nil
	}
	return decodeVal(*v)
}

func decodeVal(v valWire) Value {
	switch v.Kind {
	case "ref":
		return Ref{Path: v.Path}
	case "lit":
		return Lit{V: decodeLit(v)}
	case "object":
		fs := make([]Field, len(v.Fields))
		for i, f := range v.Fields {
			fs[i] = Field{Key: f.Key, Val: decodeVal(f.Val)}
		}
		return Object{Fields: fs}
	case "list":
		es := make([]Value, len(v.Elems))
		for i, e := range v.Elems {
			es[i] = decodeVal(e)
		}
		return List{Elems: es}
	case "template":
		ps := make([]Value, len(v.Parts))
		for i, p := range v.Parts {
			ps[i] = decodeVal(p)
		}
		return Template{Parts: ps}
	default:
		return Lit{V: nil}
	}
}

func decodeLit(v valWire) any {
	switch v.LitT {
	case "s":
		if s, ok := v.LitV.(string); ok {
			return s
		}
	case "i":
		// Decimal string (see wireLit) — lossless across the full int64 range.
		if s, ok := v.LitV.(string); ok {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
		}
	case "f":
		if f, ok := v.LitV.(float64); ok {
			return f
		}
	case "b":
		if b, ok := v.LitV.(bool); ok {
			return b
		}
	case "nil":
		return nil
	}
	return nil
}

func decodeExprPtr(e *exprWire) Expr {
	if e == nil {
		return nil
	}
	return decodeExpr(*e)
}

func decodeExpr(e exprWire) Expr {
	switch e.Kind {
	case "leaf":
		return Leaf{V: decodeValPtr(e.V)}
	case "not":
		return Not{X: decodeExprPtr(e.X)}
	case "binop":
		return BinOp{Op: e.Op, X: decodeExprPtr(e.X), Y: decodeExprPtr(e.Y)}
	default:
		return Leaf{V: Lit{V: nil}}
	}
}
