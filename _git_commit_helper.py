import os
import shutil
import subprocess

repo = r"c:\Users\IsaacGerling\linbeam"
old_file = os.path.join(repo, "install_linbeam.py")
if os.path.exists(old_file):
    os.remove(old_file)

subprocess.run(["git", "config", "user.name", "Isaac Gerling"], cwd=repo, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
subprocess.run(["git", "config", "user.email", "isaac.gerling@example.com"], cwd=repo, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
subprocess.run(["git", "add", "-A"], cwd=repo, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
subprocess.run(["git", "commit", "-m", "Add Bluebeam installer package and remove old installer"], cwd=repo, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
print(subprocess.check_output(["git", "status", "--short"], cwd=repo, text=True))
