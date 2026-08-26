import os
import sys

def replace_in_file(filepath, replacements):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    
    new_content = content
    for old, new in replacements:
        new_content = new_content.replace(old, new)
        
    if new_content != content:
        with open(filepath, 'w', encoding='utf-8') as f:
            f.write(new_content)

replacements = [
    ("RemoteFS", "file-exploder"),
    ("filequeue", "file-exploder"),
    ("FileQueue", "FileExploder"),
]

for root, dirs, files in os.walk("/home/yude/work/RemoteFS"):
    if ".git" in root or ".build" in root:
        continue
    for file in files:
        if file.endswith(".go") or file.endswith(".swift") or file.endswith(".md") or file.endswith(".sh") or file.endswith("service") or file.endswith(".mod") or file.endswith("gitignore"):
            filepath = os.path.join(root, file)
            if file == "rename.py":
                continue
            replace_in_file(filepath, replacements)

