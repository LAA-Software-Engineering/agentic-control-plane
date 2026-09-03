package lang

import "strings"

// isResourceDeclKeyword reports whether the current token opens a top-level `tool`/`policy`
// declaration. `tool` and `policy` are contextual: they are ordinary identifiers elsewhere (a grant
// path `tool.x.y`, an agent field `policy foo`), and only at the top level introduce a declaration.
func (p *parser) isResourceDeclKeyword() bool {
	return p.cur.Kind == KindIdent && (p.cur.Lit == "tool" || p.cur.Lit == "policy")
}

// parseTool parses `tool <Name> { type … safety { … } operations { … } }` (ADR 005).
func (p *parser) parseTool() *ToolDecl {
	d := &ToolDecl{Pos: p.cur.Pos}
	p.advance() // consume 'tool'
	d.Name = p.ident("after 'tool'")
	if _, ok := p.expect(KindLBrace, "to open tool body"); !ok {
		return d
	}
	seen := map[string]bool{}
	dup := func(field string, pos Pos) bool {
		if seen[field] {
			p.errorf(pos, "duplicate tool field %q (each field may appear at most once)", field)
			return true
		}
		seen[field] = true
		return false
	}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected tool field (type, mcp, http, safety, operations), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		switch field {
		case "type":
			p.advance()
			if id := p.ident("after 'type'"); !dup(field, fpos) {
				d.Type = id
			}
		case "mcp":
			p.advance()
			if b := p.parseToolMCP(); !dup(field, fpos) {
				d.MCP = b
			}
		case "http":
			p.advance()
			if b := p.parseToolHTTP(); !dup(field, fpos) {
				d.HTTP = b
			}
		case "safety":
			p.advance()
			if b := p.parseToolSafety(); !dup(field, fpos) {
				d.Safety = b
			}
		case "operations":
			p.advance()
			if ops := p.parseToolOperations(); !dup(field, fpos) {
				d.Operations = ops
			}
		default:
			p.errorf(fpos, "unknown tool field %q (want type, mcp, http, safety, or operations)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close tool body")
	return d
}

func (p *parser) parseToolSafety() *ToolSafetyBlock {
	b := &ToolSafetyBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open safety block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected a safety field (trusted, sideEffects, requiresApproval), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate safety field %q", field)
		}
		seen[field] = true
		switch field {
		case "trusted":
			if v, ok := p.constraintBool(field); ok {
				b.Trusted = &v
			}
		case "sideEffects":
			if v, ok := p.constraintBool(field); ok {
				b.SideEffects = &v
			}
		case "requiresApproval":
			if v, ok := p.constraintBool(field); ok {
				b.RequiresApproval = &v
			}
		default:
			p.errorf(fpos, "unknown safety field %q (want trusted, sideEffects, or requiresApproval)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close safety block")
	return b
}

// parseToolMCP parses `mcp { transport … command … args { … } url … headers { … } }` (issue #440).
func (p *parser) parseToolMCP() *ToolMCPBlock {
	b := &ToolMCPBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open mcp block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected an mcp field (transport, command, args, url, headers), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate mcp field %q", field)
		}
		seen[field] = true
		switch field {
		case "transport":
			b.Transport = p.parseStringLit("after 'transport'")
		case "command":
			b.Command = p.parseStringLit("after 'command'")
		case "url":
			b.URL = p.parseStringLit("after 'url'")
		case "args":
			b.Args = p.parseStringListBlock("args")
		case "headers":
			b.Headers = p.parseHeadersBlock()
		default:
			p.errorf(fpos, "unknown mcp field %q (want transport, command, args, url, or headers)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close mcp block")
	return b
}

// parseToolHTTP parses `http { baseUrl … headers { … } }` (issue #440).
func (p *parser) parseToolHTTP() *ToolHTTPBlock {
	b := &ToolHTTPBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open http block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected an http field (baseUrl, headers), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate http field %q", field)
		}
		seen[field] = true
		switch field {
		case "baseUrl":
			b.BaseURL = p.parseStringLit("after 'baseUrl'")
		case "headers":
			b.Headers = p.parseHeadersBlock()
		default:
			p.errorf(fpos, "unknown http field %q (want baseUrl or headers)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close http block")
	return b
}

// parseStringListBlock parses `<field> { "a" "b" … }` — a whitespace-separated list of string
// literals (e.g. mcp args). Order is preserved.
func (p *parser) parseStringListBlock(field string) []*StringLit {
	if _, ok := p.expect(KindLBrace, "to open "+field+" block"); !ok {
		return nil
	}
	var out []*StringLit
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindString {
			p.errorf(p.cur.Pos, "expected a string in %s { … }, got %s", field, p.cur)
			p.syncLine()
			continue
		}
		if s := p.parseStringLit("in " + field); s != nil {
			out = append(out, s)
		}
	}
	p.expect(KindRBrace, "to close "+field+" block")
	return out
}

// parseHeadersBlock parses `headers { "<key>" "<value>" … }` — string key/value pairs.
func (p *parser) parseHeadersBlock() []*HeaderPair {
	if _, ok := p.expect(KindLBrace, "to open headers block"); !ok {
		return nil
	}
	var out []*HeaderPair
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindString {
			p.errorf(p.cur.Pos, "expected a header key string in headers { … }, got %s", p.cur)
			p.syncLine()
			continue
		}
		pos := p.cur.Pos
		key := p.parseStringLit("for the header key")
		if p.cur.Kind != KindString {
			p.errorf(p.cur.Pos, "expected a value string after header key, got %s", p.cur)
			p.syncLine()
			continue
		}
		val := p.parseStringLit("for the header value")
		out = append(out, &HeaderPair{Pos: pos, Key: key, Value: val})
	}
	p.expect(KindRBrace, "to close headers block")
	return out
}

func (p *parser) parseToolOperations() *ToolOperations {
	ops := &ToolOperations{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open operations block"); !ok {
		return ops
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected an operation name, got %s", p.cur)
			p.syncLine()
			continue
		}
		op := p.parseToolOperation()
		if op != nil {
			name := identName(op.Name)
			if seen[name] {
				p.errorf(op.Pos, "duplicate operation %q", name)
			}
			seen[name] = true
			ops.Ops = append(ops.Ops, op)
		}
	}
	p.expect(KindRBrace, "to close operations block")
	return ops
}

// parseToolOperation parses `<op> { [effects { … }] }`. The op name may be dotted
// (pull_request.post_comment); its dotted form is joined into a single Ident.
func (p *parser) parseToolOperation() *ToolOperationDecl {
	parts := p.parseDottedPath("operation name")
	if len(parts) == 0 {
		p.syncLine()
		return nil
	}
	names := make([]string, len(parts))
	for i, pt := range parts {
		names[i] = pt.Name
	}
	op := &ToolOperationDecl{Pos: parts[0].Pos, Name: &Ident{Pos: parts[0].Pos, Name: strings.Join(names, ".")}}
	if _, ok := p.expect(KindLBrace, "to open operation body"); !ok {
		return op
	}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind == KindIdent && p.cur.Lit == "effects" {
			p.advance()
			op.Effects = p.parseEffects()
			continue
		}
		p.errorf(p.cur.Pos, "expected 'effects' in operation body, got %s", p.cur)
		p.syncLine()
	}
	p.expect(KindRBrace, "to close operation body")
	return op
}

// parsePolicy parses `policy <Name> { execution { … } approvals { … } effects { … } }`.
func (p *parser) parsePolicy() *PolicyDecl {
	d := &PolicyDecl{Pos: p.cur.Pos}
	p.advance() // consume 'policy'
	d.Name = p.ident("after 'policy'")
	if _, ok := p.expect(KindLBrace, "to open policy body"); !ok {
		return d
	}
	seen := map[string]bool{}
	dup := func(field string, pos Pos) bool {
		if seen[field] {
			p.errorf(pos, "duplicate policy field %q (each field may appear at most once)", field)
			return true
		}
		seen[field] = true
		return false
	}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected policy field (preset, execution, approvals, effects), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		switch field {
		case "preset":
			p.advance()
			if id := p.ident("after 'preset'"); !dup(field, fpos) {
				d.Preset = id
			}
		case "execution":
			p.advance()
			if b := p.parsePolicyExecution(); !dup(field, fpos) {
				d.Execution = b
			}
		case "approvals":
			p.advance()
			if b := p.parsePolicyApprovals(); !dup(field, fpos) {
				d.Approvals = b
			}
		case "effects":
			p.advance()
			if b := p.parsePolicyEffects(); !dup(field, fpos) {
				d.Effects = b
			}
		default:
			p.errorf(fpos, "unknown policy field %q (want preset, execution, approvals, or effects)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close policy body")
	return d
}

func (p *parser) parsePolicyExecution() *PolicyExecutionBlock {
	b := &PolicyExecutionBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open execution block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected an execution field, got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate execution field %q", field)
		}
		seen[field] = true
		switch field {
		case "maxTotalCostUsd":
			if v, ok := p.constraintFloat(field); ok {
				b.MaxTotalCostUsd = &v
			}
		case "maxWallClockSeconds":
			if v, ok := p.constraintInt(field); ok {
				b.MaxWallClockSeconds = &v
			}
		case "requireStructuredOutput":
			if v, ok := p.constraintBool(field); ok {
				b.RequireStructuredOutput = &v
			}
		default:
			p.errorf(fpos, "unknown execution field %q (want maxTotalCostUsd, maxWallClockSeconds, or requireStructuredOutput)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close execution block")
	return b
}

func (p *parser) parsePolicyApprovals() *PolicyApprovalsBlock {
	b := &PolicyApprovalsBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open approvals block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent {
			p.errorf(p.cur.Pos, "expected an approvals field (requiredFor, requireAllTools, permissive), got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate approvals field %q", field)
		}
		seen[field] = true
		switch field {
		case "requiredFor":
			b.RequiredFor = p.parseGrants()
		case "requireAllTools":
			if v, ok := p.constraintBool(field); ok {
				b.RequireAllTools = &v
			}
		case "permissive":
			if v, ok := p.constraintBool(field); ok {
				b.Permissive = &v
			}
		default:
			p.errorf(fpos, "unknown approvals field %q (want requiredFor, requireAllTools, or permissive)", field)
			p.syncLine()
		}
	}
	p.expect(KindRBrace, "to close approvals block")
	return b
}

func (p *parser) parsePolicyEffects() *PolicyEffectsBlock {
	b := &PolicyEffectsBlock{Pos: p.cur.Pos}
	if _, ok := p.expect(KindLBrace, "to open effects block"); !ok {
		return b
	}
	seen := map[string]bool{}
	for p.cur.Kind != KindRBrace && p.cur.Kind != KindEOF {
		if p.cur.Kind != KindIdent || (p.cur.Lit != "permit" && p.cur.Lit != "permitWithApproval") {
			p.errorf(p.cur.Pos, "expected 'permit' or 'permitWithApproval', got %s", p.cur)
			p.syncLine()
			continue
		}
		field, fpos := p.cur.Lit, p.cur.Pos
		p.advance()
		if seen[field] {
			p.errorf(fpos, "duplicate %q block", field)
		}
		seen[field] = true
		if field == "permit" {
			b.Permit = p.parseEffects()
		} else {
			b.PermitWithApproval = p.parseEffects()
		}
	}
	p.expect(KindRBrace, "to close effects block")
	return b
}
