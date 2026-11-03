"""Cookie Jar: an admin panel that trusts whatever the browser says.

The session is a cookie holding base64-encoded JSON, with no signature.
Change "role" to "admin" and /admin shows the flag.
"""

import base64
import html
import json
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs

FLAG = open("/app/flag.txt").read().strip()

PAGE = """<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Cookie Jar Admin</title>
<style>
 body {{ font: 16px/1.5 system-ui, sans-serif; background: #fdf6ec; color: #3b2a1a; display: grid; place-items: center; min-height: 100vh; margin: 0; }}
 main {{ background: #fff; border: 2px solid #c8a27a; border-radius: 12px; padding: 2rem 2.5rem; max-width: 30rem; }}
 h1 {{ margin-top: 0; }} input, button {{ font: inherit; padding: .4rem .6rem; }} label {{ display: block; margin: .5rem 0; }}
 .flag {{ font-family: monospace; background: #3b2a1a; color: #ffd27f; padding: .6rem; border-radius: 6px; }}
 a {{ color: #8a4b12; }}
</style></head><body><main>{body}</main></body></html>"""


def session_of(handler):
    cookie = SimpleCookie(handler.headers.get("Cookie", ""))
    if "session" not in cookie:
        return None
    try:
        return json.loads(base64.b64decode(cookie["session"].value))
    except Exception:
        return None


class Handler(BaseHTTPRequestHandler):
    server_version = "CookieJar/1.0"

    def send_page(self, body, status=200, headers=()):
        data = PAGE.format(body=body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        for k, v in headers:
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(data)

    def redirect(self, to, headers=()):
        self.send_response(303)
        self.send_header("Location", to)
        for k, v in headers:
            self.send_header(k, v)
        self.end_headers()

    def do_GET(self):
        s = session_of(self)
        if self.path == "/admin":
            if not s:
                return self.redirect("/")
            if s.get("role") == "admin":
                return self.send_page(f"<h1>Admin panel</h1><p>Welcome, administrator. Today's secret:</p><p class=flag>{FLAG}</p>")
            role = html.escape(str(s.get("role")))
            return self.send_page(f"<h1>403</h1><p>Admins only. Your session says your role is <b>{role}</b>.</p><p><a href=/>Back</a></p>", 403)
        if self.path == "/logout":
            return self.redirect("/", [("Set-Cookie", "session=; Max-Age=0; Path=/")])
        if self.path != "/":
            return self.send_page("<h1>404</h1><p><a href=/>Home</a></p>", 404)
        if s:
            user = html.escape(str(s.get("user")))
            return self.send_page(f"<h1>Hello, {user}</h1><p>You're signed in.</p><p><a href=/admin>Admin panel</a> · <a href=/logout>Sign out</a></p>")
        return self.send_page(
            "<h1>Cookie Jar Admin</h1><form method=post action=/login>"
            "<label>User <input name=user autocomplete=username></label>"
            "<label>Password <input name=password type=password autocomplete=current-password></label>"
            "<button>Sign in</button></form><p><small>Visitors: guest / guest</small></p>"
        )

    def do_POST(self):
        if self.path != "/login":
            return self.send_page("<h1>404</h1>", 404)
        n = min(int(self.headers.get("Content-Length") or 0), 4096)
        form = parse_qs(self.rfile.read(n).decode(errors="replace"))
        user, password = form.get("user", [""])[0], form.get("password", [""])[0]
        if user == "guest" and password == "guest":
            cookie = base64.b64encode(json.dumps({"user": "guest", "role": "guest"}).encode()).decode()
            return self.redirect("/", [("Set-Cookie", f"session={cookie}; Path=/; HttpOnly")])
        if user == "admin":
            return self.send_page("<h1>Nope</h1><p>Admin sign-in is disabled from the internet. Ask an admin for their session instead.</p><p><a href=/>Back</a></p>", 403)
        return self.send_page("<h1>Wrong user or password</h1><p><a href=/>Try again</a></p>", 401)

    def log_message(self, fmt, *args):
        pass  # keep the container log quiet


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
