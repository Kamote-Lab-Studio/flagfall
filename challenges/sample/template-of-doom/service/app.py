"""Template of Doom: a greeting-card generator with a server-side template
injection. The name is pasted into the template source before rendering."""

from flask import Flask, render_template_string, request

app = Flask(__name__)

HEAD = """<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Doom Cards</title>
<style>
 body { font: 16px/1.5 Georgia, serif; background: #1b1023; color: #f3e9ff; display: grid; place-items: center; min-height: 100vh; margin: 0; }
 main { text-align: center; }
 .card { background: linear-gradient(135deg, #6d2bd9, #d92b8a); padding: 2.5rem 3rem; border-radius: 16px; box-shadow: 0 10px 40px #0008; margin-bottom: 1.5rem; }
 input, button { font: inherit; padding: .4rem .7rem; border-radius: 6px; border: 0; }
 a { color: #ffb3f0; }
</style></head><body><main>"""
FOOT = "</main></body></html>"

FORM = """<h1>Doom Cards</h1>
<p>Personalized greeting cards, rendered on the server in an instant.</p>
<form><label>Your name <input name="name" autofocus></label> <button>Make my card</button></form>"""


@app.get("/")
def index():
    name = request.args.get("name", "")
    if not name:
        return render_template_string(HEAD + FORM + FOOT)
    # The bug: the name becomes part of the template itself.
    card = '<div class="card"><h1>Happy birthday, ' + name + "!</h1><p>From all of us at Doom Cards.</p></div>"
    return render_template_string(HEAD + card + '<p><a href="/">Make another</a></p>' + FOOT)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080, threaded=True)
