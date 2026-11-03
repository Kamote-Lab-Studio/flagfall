// Flagfall theme guard. The server adds this before a theme's own scripts.
//
// If the theme still shows nothing a few seconds after the page loads (its
// scripts failed to load or crashed while starting), the guard reports it
// and reloads once with a cookie that tells the server to send this browser
// the default theme for a while. Admins previewing a draft only get the
// report in the console, so they can see what broke.
(function () {
  "use strict";
  var me = document.currentScript;
  if (!me) return;
  var theme = me.getAttribute("data-theme") || "";
  var version = me.getAttribute("data-version") || "0";
  var preview = me.hasAttribute("data-preview");
  // The default theme has nothing to fall back to: it only reports.
  var isDefault = me.hasAttribute("data-default");
  var COOKIE = "flagfall_theme_fallback";
  var DEADLINE = 6000; // ms after DOMContentLoaded
  var AFTER_LOAD_ERROR = 1500; // sooner when a theme script failed to load

  var firstError = "";
  var scriptFailed = false;
  var done = false;

  function note(msg) {
    if (!firstError && msg) firstError = String(msg).slice(0, 300);
  }

  window.addEventListener(
    "error",
    function (e) {
      var t = e.target;
      if (t && t !== window && t.tagName === "SCRIPT") {
        scriptFailed = true;
        note("Couldn't load " + (t.getAttribute("src") || "a script"));
        return;
      }
      note(e.message);
    },
    true,
  );
  window.addEventListener("unhandledrejection", function (e) {
    var r = e.reason;
    note(r && r.message ? r.message : r);
  });

  // Something the player can see: text, an image, a canvas or a video.
  function showsSomething() {
    var b = document.body;
    if (!b) return false;
    if ((b.innerText || "").trim().length > 0) return true;
    var media = b.querySelectorAll("img, svg, canvas, video");
    for (var i = 0; i < media.length; i++) {
      var r = media[i].getBoundingClientRect();
      if (r.width > 0 && r.height > 0) return true;
    }
    return false;
  }

  function broken() {
    done = true;
    var reason = firstError || "The page stayed blank.";
    if (window.console) console.error("Flagfall: the " + theme + " theme didn't start: " + reason);
    if (preview) return;
    var body = JSON.stringify({ themeId: theme, version: Number(version), reason: reason, path: location.pathname });
    try {
      fetch("/api/v1/theme/failures", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: body,
        keepalive: true,
        credentials: "same-origin",
      })["catch"](function () {});
    } catch (_) {
      /* reporting is best effort */
    }
    // Reload once. The server ignores the cookie once a different theme
    // or version is published, so a fix reaches everyone; if it already
    // names this version, the server didn't honor it, so don't loop.
    var value = encodeURIComponent(theme + ":" + version);
    if (!isDefault && document.cookie.indexOf(COOKIE + "=" + value) === -1) {
      document.cookie = COOKIE + "=" + value + "; Max-Age=900; Path=/; SameSite=Lax";
      setTimeout(function () {
        location.reload();
      }, 50);
    }
  }

  function watch() {
    var start = Date.now();
    var timer = setInterval(function () {
      if (done) return clearInterval(timer);
      if (showsSomething()) {
        done = true;
        return clearInterval(timer);
      }
      var waited = Date.now() - start;
      if (waited >= DEADLINE || (scriptFailed && waited >= AFTER_LOAD_ERROR)) {
        clearInterval(timer);
        broken();
      }
    }, 250);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", watch);
  else watch();
})();
