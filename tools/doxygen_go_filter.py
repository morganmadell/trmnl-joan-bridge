#!/usr/bin/env python3
"""Doxygen input filter: turns a Go source file into C-like declarations.

Doxygen cannot parse Go. This filter keeps the doc comments (// blocks that
contain @brief) and the declaration that follows each one, and drops function
bodies, so Doxygen can build the API reference. It is a text filter, not a Go
parser: it handles the declaration shapes used in this repository.

Usage (set in Doxyfile): FILTER_PATTERNS = *.go="python tools/doxygen_go_filter.py"
"""
import re
import sys


def c_type(t):
    t = t.strip()
    t = re.sub(r'^\[\](.+)$', r'\1[]', t)
    t = re.sub(r'^\*(.+)$', r'\1*', t)
    return t or 'void'


def split_top(s):
    """Split on commas that are not inside brackets."""
    out, depth, cur = [], 0, ''
    for ch in s:
        if ch in '([{':
            depth += 1
        elif ch in ')]}':
            depth -= 1
        if ch == ',' and depth == 0:
            out.append(cur)
            cur = ''
        else:
            cur += ch
    if cur.strip():
        out.append(cur)
    return [x.strip() for x in out]


def c_params(plist):
    """Go 'a, b int, c string' -> C 'int a, int b, string c'."""
    items = [re.sub(r'^(\w+)\s+func\(.*$', r'\1 callback', i) for i in split_top(plist)]
    out, pending = [], None
    for it in reversed(items):
        parts = it.split(None, 1)
        if len(parts) == 2:
            name, typ = parts
            out.append('%s %s' % (c_type(typ), name))
            pending = typ
        elif pending:
            out.append('%s %s' % (c_type(pending), parts[0]))
        else:
            out.append(c_type(parts[0]))
    return ', '.join(reversed(out))


def c_ret(ret):
    ret = ret.strip().rstrip('{').strip()
    if not ret:
        return 'void'
    if ret.startswith('('):
        inner = split_top(ret[1:-1])
        types = [c_type(p.split(None, 1)[1] if len(p.split(None, 1)) == 2 else p) for p in inner]
        return 'tuple<%s>' % ', '.join(types)
    return c_type(ret)


HEAD = re.compile(r'^func\s*(?:\((?:\w+\s+)?\*?(\w+)\)\s*)?(\w+)\s*\(')


def parse_func(line):
    """Return (receiver, name, params, ret) or None. Balances parentheses so that
    func-typed parameters do not end the parameter list early."""
    m = HEAD.match(line)
    if not m:
        return None
    depth, k = 1, m.end()
    while k < len(line) and depth:
        depth += {'(': 1, ')': -1}.get(line[k], 0)
        k += 1
    params = line[m.end():k - 1]
    ret = line[k:].rstrip().rstrip('{').strip()
    return m.group(1), m.group(2), params, ret


def main(path):
    lines = open(path, encoding='utf-8').read().replace('\r\n', '\n').split('\n')
    free, types, members = [], {}, {}
    i = 0
    while i < len(lines):
        if not lines[i].startswith('//'):
            i += 1
            continue
        j = i
        while j < len(lines) and lines[j].startswith('//'):
            j += 1
        block = [x[2:].lstrip() if x.startswith('// ') else x[2:] for x in lines[i:j]]
        nxt = lines[j] if j < len(lines) else ''
        if any(b.startswith('@brief') for b in block):
            doc = ['/**'] + [' * ' + b for b in block] + [' */']
            m = parse_func(nxt)
            mt = re.match(r'^type\s+(\w+)\s+(struct|interface)', nxt)
            mv = re.match(r'^var\s+(\w+)', nxt)
            if m:
                cls, name, params, ret = m
                decl = '%s %s(%s);' % (c_ret(ret), name, c_params(params))
                if cls:
                    members.setdefault(cls, []).extend(doc + [decl])
                else:
                    free.extend(doc + [decl])
            elif mt:
                types[mt.group(1)] = (doc, 'struct' if mt.group(2) == 'struct' else 'class')
            elif mv:
                free.extend(doc + ['extern const unsigned char %s[];' % mv.group(1)])
        i = j
    out = list(free)
    for name, (doc, kind) in types.items():
        out += doc + ['%s %s {' % (kind, name)] + members.get(name, []) + ['};']
    sys.stdout.buffer.write(('\n'.join(out) + '\n').encode('utf-8'))


if __name__ == '__main__':
    main(sys.argv[1])
