import re
import os

def fix_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()

    original = content
    # Look for `r.wg.Go(func() {` or `wg.Go(func() {`

    # We will replace `wg.Go(func() {` with `wg.Add(1)\n\tgo func() {\n\t\tdefer wg.Done()`
    # And we will also replace the closing `})` that corresponds to it with `}()`
    # But replacing `})` globally is dangerous. Let's do it carefully.

    lines = content.split('\n')
    out_lines = []
    stack = []

    for i, line in enumerate(lines):
        if 'r.wg.Go(func() {' in line:
            line = line.replace('r.wg.Go(func() {', 'r.wg.Add(1)\n\tgo func() {\n\t\tdefer r.wg.Done()')
            out_lines.append(line)
            # Find the matching closing brace. Since we know it's formatted well,
            # we just need to look for `	})` with the same indentation.
            indent = re.match(r'^\s*', lines[i]).group(0)
            stack.append(indent + '})')
            continue
        elif 'wg.Go(func() {' in line:
            indent = re.match(r'^\s*', lines[i]).group(0)
            line = line.replace('wg.Go(func() {', 'wg.Add(1)\n' + indent + 'go func() {\n' + indent + '\tdefer wg.Done()')
            out_lines.append(line)
            stack.append(indent + '})')
            continue

        if len(stack) > 0 and stack[-1] == line:
            out_lines.append(line.replace('})', '}(') + ')')
            stack.pop()
        else:
            out_lines.append(line)

    if out_lines != lines:
        with open(filepath, 'w') as f:
            f.write('\n'.join(out_lines))
        print(f"Fixed {filepath}")

for root, dirs, files in os.walk('.'):
    for file in files:
        if file.endswith('.go'):
            fix_file(os.path.join(root, file))
