// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package initconfig

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ErrInvalidConfig is returned when an existing configuration file cannot be parsed as a JSON/JSONC object.
var ErrInvalidConfig = errors.New("invalid MCP configuration file")

func marshalNewConfig(entryKey string, spec entrySpec) ([]byte, error) {
	var doc any = map[string]any{spec.parentKey: map[string]any{entryKey: spec.entryVal}}
	if spec.newFileSchema != "" && spec.parentKey == "mcp" {
		doc = struct {
			Schema string         `json:"$schema"`
			MCP    map[string]any `json:"mcp"`
		}{spec.newFileSchema, map[string]any{entryKey: spec.entryVal}}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	return append(out, '\n'), err
}

func managedFieldsMatch(entryVal any, desired map[string]any) bool {
	em, ok := entryVal.(map[string]any)
	for k, want := range desired {
		if !ok || !reflect.DeepEqual(em[k], want) {
			return false
		}
	}
	return ok
}

type (
	tok struct {
		s, e int
		k    byte
	}
	propSpan struct {
		key                        string
		keyStart, valStart, valEnd int
		hasComma                   bool
		commaIndex                 int
	}
	objSpan struct {
		openPos, closePos int
		props             []propSpan
	}
)

func nextToken(src []byte, pos int) (tok, error) {
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case ' ', '\t', '\r', '\n':
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				if nl := bytes.IndexByte(src[i+2:], '\n'); nl >= 0 {
					i += 1 + nl
					continue
				}
				return tok{s: len(src), e: len(src)}, nil
			}
			if i+1 >= len(src) || src[i+1] != '*' {
				return tok{s: i, e: i + 1, k: 'p'}, nil
			}
			cEnd := bytes.Index(src[i+2:], []byte("*/"))
			if cEnd < 0 {
				return tok{}, fmt.Errorf("%w: unterminated block comment", ErrInvalidConfig)
			}
			i += 3 + cEnd
		case '"':
			for j := i + 1; j < len(src); j++ {
				if src[j] < 0x20 {
					return tok{}, fmt.Errorf("%w: unescaped control character in string", ErrInvalidConfig)
				}
				if src[j] == '"' {
					return tok{s: i, e: j + 1, k: '"'}, nil
				}
				if src[j] == '\\' {
					j++
				}
			}
			return tok{}, fmt.Errorf("%w: unterminated string literal", ErrInvalidConfig)
		case '{', '}', '[', ']', ':', ',':
			return tok{s: i, e: i + 1, k: src[i]}, nil
		default:
			j := i
			for j < len(src) && !strings.ContainsRune(" \t\r\n,{}[]:/", rune(src[j])) {
				j++
			}
			return tok{s: i, e: j, k: 'p'}, nil
		}
	}
	return tok{s: len(src), e: len(src)}, nil
}

func parseAndVerifyJSONC(src []byte, parentKey string) (map[string]any, error) {
	clean := make([]byte, 0, len(src))
	var prevKind byte
	for prevEnd := 0; ; {
		tk, err := nextToken(src, prevEnd)
		if err != nil || tk.k == 0 {
			if err != nil {
				return nil, err
			}
			break
		}
		if tk.s > prevEnd && len(clean) > 0 {
			clean = append(clean, ' ')
		}
		if tk.k == ',' {
			nt, nErr := nextToken(src, tk.e)
			if nErr != nil {
				return nil, nErr
			}
			if (nt.k == '}' || nt.k == ']') && !strings.ContainsRune("\x00{[,", rune(prevKind)) {
				prevEnd = tk.e
				continue
			}
		}
		clean = append(clean, src[tk.s:tk.e]...)
		prevKind, prevEnd = tk.k, tk.e
	}
	var rawVal any
	if err := json.Unmarshal(clean, &rawVal); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	root, ok := rawVal.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: root JSON value must be an object", ErrInvalidConfig)
	}
	if pv, exists := root[parentKey]; exists {
		if _, isObj := pv.(map[string]any); !isObj {
			return nil, fmt.Errorf("%w: %q must be a JSON object", ErrInvalidConfig, parentKey)
		}
	}
	return root, nil
}

func parseObjectSpan(src []byte, openPos int) objSpan {
	res := objSpan{openPos: openPos}
	for pos := openPos + 1; ; {
		kt, _ := nextToken(src, pos)
		if kt.k == '}' || kt.k == 0 {
			res.closePos = kt.s
			return res
		}
		var key string
		_ = json.Unmarshal(src[kt.s:kt.e], &key)
		ct, _ := nextToken(src, kt.e)
		vt, _ := nextToken(src, ct.e)
		ve := vt.e
		for depth := 1; (vt.k == '{' || vt.k == '[') && depth > 0; {
			nt, _ := nextToken(src, ve)
			ve = nt.e
			switch nt.k {
			case '{', '[':
				depth++
			case '}', ']', 0:
				depth--
			}
		}
		prop := propSpan{key: key, keyStart: kt.s, valStart: vt.s, valEnd: ve}
		if nt, _ := nextToken(src, ve); nt.k == ',' {
			prop.hasComma, prop.commaIndex, pos = true, nt.s, nt.e
		} else {
			pos = nt.s
		}
		res.props = append(res.props, prop)
	}
}

func findLastProp(props []propSpan, key string) *propSpan {
	for i := len(props) - 1; i >= 0; i-- {
		if props[i].key == key {
			return &props[i]
		}
	}
	return nil
}

func lineLeadingIndent(src []byte, pos int) string {
	sub := src[bytes.LastIndexByte(src[:pos], '\n')+1 : pos]
	return string(sub[:len(sub)-len(bytes.TrimLeft(sub, " \t"))])
}

func insertPropertyInObject(src []byte, obj objSpan, step, propKey string, val any) []byte {
	outer := lineLeadingIndent(src, obj.openPos)
	indent := outer + step
	if len(obj.props) > 0 && bytes.Contains(src[obj.openPos:obj.props[0].keyStart], []byte{'\n'}) {
		indent = lineLeadingIndent(src, obj.props[0].keyStart)
	}
	kj, _ := json.Marshal(propKey)
	vj, _ := json.MarshalIndent(val, indent, step)
	entry := "\n" + indent + string(kj) + ": " + string(vj)
	if len(obj.props) == 0 {
		if inner := src[obj.openPos+1 : obj.closePos]; len(bytes.TrimSpace(inner)) == 0 {
			return slices.Concat(src[:obj.openPos+1], []byte(entry+"\n"+outer), src[obj.closePos:])
		} else if inner[len(inner)-1] == '\n' {
			entry = entry[1:]
		}
		return slices.Concat(src[:obj.closePos], []byte(entry+"\n"+outer), src[obj.closePos:])
	}
	last := obj.props[len(obj.props)-1]
	pos := last.valEnd
	if last.hasComma {
		pos = last.commaIndex + 1
	}
	for pos < obj.closePos && src[pos] != '\n' && src[pos] != '\r' {
		if pos+1 < obj.closePos && src[pos] == '/' && src[pos+1] == '/' {
			for pos < obj.closePos && src[pos] != '\n' && src[pos] != '\r' {
				pos++
			}
			break
		}
		if pos+1 < obj.closePos && src[pos] == '/' && src[pos+1] == '*' {
			if end := bytes.Index(src[pos+2:obj.closePos], []byte("*/")); end >= 0 {
				pos += 4 + end
				continue
			}
		}
		pos++
	}
	head, tail := src[:pos], ""
	if !last.hasComma {
		head = slices.Concat(src[:last.valEnd], []byte{','}, src[last.valEnd:pos])
	}
	if !bytes.Contains(src[pos:obj.closePos], []byte{'\n'}) {
		tail = "\n" + outer
	}
	return slices.Concat(head, []byte(entry+tail), src[pos:])
}

func upsertJSONC(existing []byte, existed bool, entryKey string, spec entrySpec) ([]byte, error) {
	if !existed {
		return marshalNewConfig(entryKey, spec)
	}
	rootMap, err := parseAndVerifyJSONC(existing, spec.parentKey)
	if err != nil {
		return nil, err
	}
	pm, _ := rootMap[spec.parentKey].(map[string]any)
	if pm != nil && managedFieldsMatch(pm[entryKey], spec.entryVal) {
		return existing, nil
	}

	rt, _ := nextToken(existing, 0)
	rootObj := parseObjectSpan(existing, rt.s)
	var detectedStep string
	if len(rootObj.props) > 0 && bytes.Contains(existing[rootObj.openPos:rootObj.props[0].keyStart], []byte{'\n'}) {
		detectedStep = lineLeadingIndent(existing, rootObj.props[0].keyStart)
	}
	step := cmp.Or(detectedStep, "  ")
	replaceVal := func(buf []byte, p propSpan, v any) []byte {
		vj, _ := json.MarshalIndent(v, lineLeadingIndent(buf, p.keyStart), step)
		return slices.Concat(buf[:p.valStart], vj, buf[p.valEnd:])
	}

	var updated []byte
	if parentProp := findLastProp(rootObj.props, spec.parentKey); parentProp == nil {
		updated = insertPropertyInObject(existing, rootObj, step, spec.parentKey, map[string]any{entryKey: spec.entryVal})
	} else {
		parentObj := parseObjectSpan(existing, parentProp.valStart)
		entryProp := findLastProp(parentObj.props, entryKey)
		entryMap, isObj := pm[entryKey].(map[string]any)
		switch {
		case entryProp == nil:
			updated = insertPropertyInObject(existing, parentObj, step, entryKey, spec.entryVal)
		case isObj && existing[entryProp.valStart] == '{':
			updated = existing
			for _, k := range spec.fieldKeys {
				if want := spec.entryVal[k]; !reflect.DeepEqual(entryMap[k], want) {
					eObj := parseObjectSpan(updated, entryProp.valStart)
					if fp := findLastProp(eObj.props, k); fp != nil {
						updated = replaceVal(updated, *fp, want)
					} else {
						updated = insertPropertyInObject(updated, eObj, step, k, want)
					}
				}
			}
		default:
			updated = replaceVal(existing, *entryProp, spec.entryVal)
		}
	}

	if len(updated) > 0 && updated[len(updated)-1] != '\n' {
		updated = append(updated, '\n')
	}
	verified, err := parseAndVerifyJSONC(updated, spec.parentKey)
	if vpm, ok := verified[spec.parentKey].(map[string]any); err != nil || !ok || !managedFieldsMatch(vpm[entryKey], spec.entryVal) {
		return nil, fmt.Errorf("self-verifying updated JSONC (%s.%s): %w", spec.parentKey, entryKey, cmp.Or(err, ErrInvalidConfig))
	}
	return updated, nil
}
