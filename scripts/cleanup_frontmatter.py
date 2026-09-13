import glob
import re
import os
import sys

def extract_frontmatter(content):
    lines = content.split('\n')
    if len(lines) < 6:
        return None

    id_match = re.match(r'^# (\S+)', lines[0])
    if not id_match:
        return None

    author_match = re.match(r'\*\*Author\*\*: (.+) \((\S+)\)', lines[2])
    if not author_match:
        return None

    ts_match = re.match(r'\*\*Timestamp\*\*: (.+)', lines[3])
    if not ts_match:
        return None

    channel_match = re.match(r'\*\*Channel\*\*: (.+)', lines[4])
    if not channel_match:
        return None

    return {
        'message_id': id_match.group(1),
        'author_name': author_match.group(1),
        'author_id': author_match.group(2),
        'timestamp': ts_match.group(1),
        'channel': channel_match.group(1),
    }

def find_first_separator(content):
    lines = content.split('\n')
    for i, line in enumerate(lines):
        if line.strip() == '---':
            return i
    return None

def new_frontmatter(data):
    return f"""---
message_id: "{data['message_id']}"
author_name: "{data['author_name']}"
author_id: "{data['author_id']}"
timestamp: "{data['timestamp']}"
channel: "{data['channel']}"
---
"""

def main():
    files = sorted(glob.glob('output/*.md'))
    converted = 0
    skipped = 0
    errors = []

    for fpath in files:
        fname = os.path.basename(fpath)
        try:
            with open(fpath, 'r') as f:
                content = f.read()
        except Exception as e:
            errors.append((fname, f'read error: {e}'))
            continue

        data = extract_frontmatter(content)
        if data is None:
            skipped += 1
            continue

        sep_idx = find_first_separator(content)
        if sep_idx is None:
            skipped += 1
            continue

        lines = content.split('\n')
        body = lines[sep_idx + 1:]

        new_content = new_frontmatter(data) + '\n'.join(body)

        try:
            with open(fpath, 'w') as f:
                f.write(new_content)
            converted += 1
        except Exception as e:
            errors.append((fname, f'write error: {e}'))

    print(f"Converted: {converted}")
    print(f"Skipped: {skipped}")
    if errors:
        print(f"Errors ({len(errors)}):")
        for fname, err in errors:
            print(f"  {fname}: {err}")

    return 0 if not errors else 1

if __name__ == '__main__':
    sys.exit(main())